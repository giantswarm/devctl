package rolloutwait

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// A pull request that releases nothing (a configuration change) rolls out
// as its merge commit: a Flux GitRepository of the repository fetches it,
// and the Kustomizations and Konfigurations that read the GitRepository
// apply it. A Konfiguration renders a ConfigMap and a Secret per app,
// labelled with the revision, and the HelmReleases that take their values
// from them upgrade when the values changed: those of the apps the pull
// request changed are followed too. A Kustomization applies objects as
// they are in the repository: the HelmReleases it changed, or whose values
// or chart source it changed, are followed as well.

// GitHub answers whether a commit contains another; *githubclient.Client
// is one.
type GitHub interface {
	// Reachable says whether ref is in the history of branch, a branch or
	// a commit.
	Reachable(ctx context.Context, owner, repo, ref, branch string) (bool, error)
}

// NotFetchedError says that nothing on the installation fetches the
// repository: a pull request of it that releases nothing does not roll out
// there.
type NotFetchedError struct{ Reason string }

func (e *NotFetchedError) Error() string { return e.Reason }

// ExitCode places it in the exit-code table: not applicable.
func (e *NotFetchedError) ExitCode() int { return agentcli.ExitNotApplicable }

// ExitVerdict is no_release: neither a release nor a configuration source
// follows the pull request.
func (e *NotFetchedError) ExitVerdict() agentcli.Verdict { return agentcli.VerdictNoRelease }

// The kinds of object a revision rolls out through.
const (
	KindGitRepository = "GitRepository"
	KindKustomization = "Kustomization"
	KindKonfiguration = "Konfiguration"
	// KindConfigMap is the values a Konfiguration renders for a changed
	// app that no HelmRelease or App takes.
	KindConfigMap = "ConfigMap"
	kindSecret    = "Secret"
)

var (
	gitRepositories = schema.GroupVersionResource{Group: groupSource, Version: "v1", Resource: "gitrepositories"}
	kustomizations  = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	konfigurations  = schema.GroupVersionResource{Group: "konfigure.giantswarm.io", Version: "v1alpha1", Resource: "konfigurations"}
	configMaps      = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	secrets         = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	charts          = apps.GroupVersion().WithResource("charts")
)

// labelRevision is the label konfigure-operator puts on what it renders:
// the commit of the configuration it rendered from.
const labelRevision = "configuration.giantswarm.io/revision"

// The Ready reasons of a Kustomization whose last attempt failed.
var kustomizationFailures = map[string]bool{
	"ArtifactFailed":       true,
	"BuildFailed":          true,
	"HealthCheckFailed":    true,
	"PruneFailed":          true,
	"ReconciliationFailed": true,
}

// observeRevision reads the GitRepositories of the repository and what
// applies them, judged against the merge commit.
func (w *Waiter) observeRevision(ctx context.Context) ([]Deployment, error) {
	items, err := w.list(ctx, gitRepositories, "")
	if err != nil {
		return nil, err
	}
	var list []Deployment
	var sources []object
	for i := range items {
		g := &items[i]
		if !w.fetches(str(g, "spec", "url")) {
			continue
		}
		d, err := w.judgeGitRepository(ctx, g)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
		sources = append(sources, object{kind: KindGitRepository, namespace: g.GetNamespace(), name: g.GetName()})
	}
	if len(list) == 0 {
		return nil, &NotFetchedError{Reason: fmt.Sprintf("no Flux GitRepository on %s fetches %s, so nothing there applies %s", w.installation, w.repository, short(w.revision))}
	}

	consumers, err := w.revisionConsumers(ctx, sources)
	if err != nil {
		return nil, err
	}
	if len(consumers) == 0 {
		return nil, agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "no Kustomization or Konfiguration on %s reads %s (a GitRepository included into another is not followed)", w.installation, ids(list))
	}
	return append(list, consumers...), nil
}

// revisionConsumers are the Kustomizations and Konfigurations that read one
// of the sources, the HelmReleases that take their values from what a
// Konfiguration renders for an app the pull request changed, the
// HelmReleases a Kustomization changed with the revision (their spec, a
// ConfigMap or Secret of their valuesFrom, their chart source), and those
// --helmrelease names.
func (w *Waiter) revisionConsumers(ctx context.Context, sources []object) ([]Deployment, error) {
	ks, err := w.list(ctx, kustomizations, "")
	if err != nil {
		return nil, err
	}
	var list []Deployment
	// applied are the objects the Kustomizations apply: inventory ID to
	// the Kustomization.
	applied := map[string]string{}
	for i := range ks {
		k := &ks[i]
		if str(k, "spec", "sourceRef", "kind") != KindGitRepository || !slices.Contains(sources, sourceRef(k, "spec", "sourceRef")) {
			continue
		}
		d, err := w.judgeKustomization(ctx, k)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
		entries, _, _ := unstructured.NestedSlice(k.Object, "status", "inventory", "entries")
		for _, e := range entries {
			if m, ok := e.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					applied[id] = d.id()
				}
			}
		}
	}

	kfgs, err := w.list(ctx, konfigurations, "")
	if err != nil {
		return nil, err
	}
	var rendered []renderedValues
	for i := range kfgs {
		k := &kfgs[i]
		if !slices.Contains(sources, sourceRef(k, "spec", "sources", "flux", "gitRepository")) {
			continue
		}
		d, err := w.judgeKonfiguration(ctx, k)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
		rendered = append(rendered, w.renderedFor(k)...)
	}

	// followed are the rendered values a HelmRelease or an App takes.
	followed := map[string]bool{}
	hrs, err := w.list(ctx, helmReleases, "")
	if err != nil {
		return nil, err
	}
	w.seenHelmReleases = len(hrs)
	for i := range hrs {
		hr := &hrs[i]
		judged := false
		for _, r := range rendered {
			if r.namespace == hr.GetNamespace() && takesValuesFrom(hr, r.name) {
				followed[r.id()] = true
				if !r.touched && !w.named(hr.GetNamespace(), hr.GetName()) {
					continue
				}
				d, err := w.judgeValuesHelmRelease(ctx, hr, r)
				if err != nil {
					return nil, err
				}
				list = append(list, d)
				judged = true
				break
			}
		}
		if judged {
			continue
		}
		d, follow, err := w.judgeAppliedHelmRelease(ctx, hr, applied)
		if err != nil {
			return nil, err
		}
		if follow {
			list = append(list, d)
		}
	}
	if len(rendered) == 0 {
		return list, nil
	}

	items, err := w.list(ctx, apps, "")
	if err != nil {
		return nil, err
	}
	for i := range items {
		app := &items[i]
		for _, r := range rendered {
			if appTakesValuesFrom(app, r) {
				followed[r.id()] = true
				if !r.touched {
					continue
				}
				d, err := w.judgeValuesApp(ctx, app, r)
				if err != nil {
					return nil, err
				}
				list = append(list, d)
				break
			}
		}
	}

	// The values of a changed app that nothing takes: whether the change
	// runs cannot be told, so it is not reported rolled out.
	for _, r := range rendered {
		if r.touched && !followed[r.id()] {
			d := Deployment{Kind: KindConfigMap, Namespace: r.namespace, Name: r.name, Follows: r.konfiguration.String(), Workloads: []Workload{}}
			list = append(list, d.set(StateUnknown, "no HelmRelease or App on %s takes these values: whether the change runs there cannot be told", w.installation))
		}
	}
	return list, nil
}

// sourceRef is the object a reference at fields names (kind GitRepository),
// defaulting to the referrer's namespace.
func sourceRef(u *unstructured.Unstructured, fields ...string) object {
	ns := str(u, append(fields, "namespace")...)
	if ns == "" {
		ns = u.GetNamespace()
	}
	return object{kind: KindGitRepository, namespace: ns, name: str(u, append(fields, "name")...)}
}

// fetches says whether a GitRepository URL is the repository's, over ssh or
// https, with or without .git.
func (w *Waiter) fetches(url string) bool {
	url = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(url), "/"), ".git")
	repository := strings.ToLower(w.repository)
	return strings.Contains(url, "github.com") && (strings.HasSuffix(url, "/"+repository) || strings.HasSuffix(url, ":"+repository))
}

// applied says whether a revision as Flux or konfigure-operator reports it
// (main@sha1:<sha>, sha1:<sha>, main/<sha>, <sha>) contains the merge
// commit; the answers are kept, a commit's history does not change.
func (w *Waiter) applied(ctx context.Context, revision string) (bool, error) {
	sha := revisionSHA(revision)
	if sha == "" {
		return false, nil
	}
	if sha == w.revision {
		return true, nil
	}
	if known, ok := w.contains[sha]; ok {
		return known, nil
	}
	owner, repo, _ := strings.Cut(w.repository, "/")
	reachable, err := w.github.Reachable(ctx, owner, repo, w.revision, sha)
	if err != nil {
		return false, fmt.Errorf("comparing %s with %s in %s: %w", short(w.revision), short(sha), w.repository, err)
	}
	w.contains[sha] = reachable
	return reachable, nil
}

func revisionSHA(revision string) string {
	revision = revision[strings.LastIndex(revision, ":")+1:]
	return revision[strings.LastIndex(revision, "/")+1:]
}

func (w *Waiter) judgeGitRepository(ctx context.Context, g *unstructured.Unstructured) (Deployment, error) {
	revision := str(g, "status", "artifact", "revision")
	d := Deployment{
		Kind: KindGitRepository, Namespace: g.GetNamespace(), Name: g.GetName(),
		Source: str(g, "spec", "url"), Follows: gitRef(g), RunningVersion: short(revisionSHA(revision)),
		Workloads: []Workload{}, self: &object{resource: gitRepositories, kind: KindGitRepository, namespace: g.GetNamespace(), name: g.GetName()},
	}
	if suspended, _, _ := unstructured.NestedBool(g.Object, "spec", "suspend"); suspended {
		return d.set(StateSuspended, "the GitRepository is suspended: flux resume source git -n %s %s", d.Namespace, d.Name), nil
	}
	ok, err := w.applied(ctx, revision)
	if err != nil || ok {
		return d.set(StateRolledOut, ""), err
	}
	return d.set(StateProgressing, "fetched %s; waiting for %s (the GitRepository: %s)", orNone(d.RunningVersion), short(w.revision), orUnknown(condition(g, "Ready"))), nil
}

// gitRef is the ref a GitRepository fetches.
func gitRef(g *unstructured.Unstructured) string {
	for _, k := range []string{"commit", "tag", "semver", "name", "branch"} {
		if v := str(g, "spec", "ref", k); v != "" {
			return k + " " + v
		}
	}
	return "branch master"
}

func (w *Waiter) judgeKustomization(ctx context.Context, k *unstructured.Unstructured) (Deployment, error) {
	d := Deployment{
		Kind: KindKustomization, Namespace: k.GetNamespace(), Name: k.GetName(),
		Source: sourceRef(k, "spec", "sourceRef").String(), Follows: "path " + str(k, "spec", "path"),
		RunningVersion: short(revisionSHA(str(k, "status", "lastAppliedRevision"))),
		Workloads:      []Workload{}, self: &object{resource: kustomizations, kind: KindKustomization, namespace: k.GetNamespace(), name: k.GetName()},
	}
	if suspended, _, _ := unstructured.NestedBool(k.Object, "spec", "suspend"); suspended {
		return d.set(StateSuspended, "the Kustomization is suspended: flux resume kustomization -n %s %s", d.Namespace, d.Name), nil
	}
	return w.judgeApplier(ctx, d, k, str(k, "status", "lastAppliedRevision"), str(k, "status", "lastAttemptedRevision"), func(c cond) bool { return kustomizationFailures[c.reason] })
}

func (w *Waiter) judgeKonfiguration(ctx context.Context, k *unstructured.Unstructured) (Deployment, error) {
	d := Deployment{
		Kind: KindKonfiguration, Namespace: k.GetNamespace(), Name: k.GetName(),
		Source: sourceRef(k, "spec", "sources", "flux", "gitRepository").String(), Follows: "the GitRepository",
		RunningVersion: short(revisionSHA(str(k, "status", "lastAppliedRevision"))), Workloads: []Workload{},
	}
	if suspended, _, _ := unstructured.NestedBool(k.Object, "spec", "reconciliation", "suspend"); suspended {
		return d.set(StateSuspended, "the Konfiguration is suspended"), nil
	}
	// konfigure-operator reports a failed render as Ready False with the
	// attempted revision ahead of the applied one.
	return w.judgeApplier(ctx, d, k, str(k, "status", "lastAppliedRevision"), str(k, "status", "lastAttemptedRevision"), func(cond) bool { return true })
}

// judgeApplier judges an object that applies a revision: rolled out once
// it applied one that contains the merge commit and is ready at its
// generation, failed when its attempt at such a revision failed.
func (w *Waiter) judgeApplier(ctx context.Context, d Deployment, u *unstructured.Unstructured, applied, attempted string, failure func(cond) bool) (Deployment, error) {
	ready := condition(u, "Ready")
	current := num(u, "status", "observedGeneration") >= u.GetGeneration()
	done, err := w.applied(ctx, applied)
	if err != nil {
		return d, err
	}
	if done && current && ready.status == conditionTrue {
		return d.set(StateRolledOut, ""), nil
	}
	if !done && ready.status == conditionFalse && failure(ready) {
		tried, err := w.applied(ctx, attempted)
		if err != nil {
			return d, err
		}
		if tried {
			return d.set(StateFailed, "%s %s: %s", ready.reason, short(revisionSHA(attempted)), ready.message), nil
		}
	}
	return d.set(StateProgressing, "applied %s; waiting for %s (%s)", orNone(d.RunningVersion), short(w.revision), orUnknown(ready)), nil
}

// renderedValues is the ConfigMap (and Secret of the same name) a
// Konfiguration renders for one app.
type renderedValues struct {
	konfiguration   object
	namespace, name string
	// touched: the pull request changed the app's files.
	touched bool
}

func (r renderedValues) id() string { return r.namespace + "/" + r.name }

// renderedFor are the values a Konfiguration renders, one per iteration
// (app), named <prefix>-<app>-<suffix>.
func (w *Waiter) renderedFor(k *unstructured.Unstructured) []renderedValues {
	iterations, _, _ := unstructured.NestedMap(k.Object, "spec", "targets", "iterations")
	namespace := str(k, "spec", "destination", "namespace")
	if namespace == "" {
		namespace = k.GetNamespace()
	}
	separator := ""
	if sep, _, _ := unstructured.NestedBool(k.Object, "spec", "destination", "naming", "useSeparator"); sep {
		separator = "-"
	}
	var out []renderedValues
	for app := range iterations {
		var parts []string
		for _, p := range []string{str(k, "spec", "destination", "naming", "prefix"), app, str(k, "spec", "destination", "naming", "suffix")} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		out = append(out, renderedValues{
			konfiguration: object{kind: KindKonfiguration, namespace: k.GetNamespace(), name: k.GetName()},
			namespace:     namespace,
			name:          strings.Join(parts, separator),
			touched:       w.touched[app],
		})
	}
	return out
}

// takesValuesFrom says whether a HelmRelease reads a ConfigMap or Secret
// of the name in its valuesFrom.
func takesValuesFrom(hr *unstructured.Unstructured, name string) bool {
	list, _, _ := unstructured.NestedSlice(hr.Object, "spec", "valuesFrom")
	for _, item := range list {
		m, ok := item.(map[string]any)
		if ok && m["name"] == name && (m["kind"] == KindConfigMap || m["kind"] == kindSecret) {
			return true
		}
	}
	return false
}

// judgeValuesHelmRelease judges a HelmRelease whose values a Konfiguration
// renders: rolled out once it deployed after the ConfigMap took the merge
// commit, took new values after the merge, or handled the reconcile
// --reconcile requested after it did (which reads the values as they are),
// is ready and its workloads are rolled out.
func (w *Waiter) judgeValuesHelmRelease(ctx context.Context, hr *unstructured.Unstructured, r renderedValues) (Deployment, error) {
	d := Deployment{
		Kind: KindHelmRelease, Namespace: hr.GetNamespace(), Name: hr.GetName(),
		Source: "ConfigMap " + r.namespace + "/" + r.name, Follows: r.konfiguration.String(), Workloads: []Workload{},
	}
	d, last := lastDeploy(d, hr)
	if suspended, _, _ := unstructured.NestedBool(hr.Object, "spec", "suspend"); suspended {
		return d.set(StateSuspended, "the HelmRelease is suspended: flux resume helmrelease -n %s %s", d.Namespace, d.Name), nil
	}

	cm, err := w.client.Resource(configMaps).Namespace(r.namespace).Get(ctx, r.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return d.set(StateProgressing, "%s has not rendered %s yet", r.konfiguration, d.Source), nil
	}
	if err != nil {
		return d, w.clusterErr("reading "+d.Source, err)
	}
	renderedFrom := cm.GetLabels()[labelRevision]
	ok, err := w.applied(ctx, renderedFrom)
	if err != nil {
		return d, err
	}
	if !ok {
		return d.set(StateProgressing, "%s rendered from %s; waiting for %s", d.Source, orNone(short(renderedFrom)), short(w.revision)), nil
	}
	return w.judgeNewValues(ctx, d, hr, last, lastWrite(cm), w.helmReleaseTookNewValues(hr))
}

// deployed is a HelmRelease's last successful deploy: its Helm release and
// when.
type deployed struct {
	release, namespace string
	at                 time.Time
}

// lastDeploy fills the chart and the running version of a HelmRelease from
// its last deploy, when that succeeded.
func lastDeploy(d Deployment, hr *unstructured.Unstructured) (Deployment, deployed) {
	var last deployed
	if history, _, _ := unstructured.NestedSlice(hr.Object, "status", "history"); len(history) > 0 {
		if h, ok := history[0].(map[string]any); ok && h["status"] == statusDeployed {
			d.Chart, _ = h["chartName"].(string)
			v, _ := h["chartVersion"].(string)
			d.RunningVersion = bare(v)
			last.release, _ = h["name"].(string)
			last.namespace, _ = h["namespace"].(string)
			at, _ := h["lastDeployed"].(string)
			last.at, _ = time.Parse(time.RFC3339, at)
		}
	}
	return d, last
}

// judgeNewValues judges a HelmRelease whose values (d.Source) changed with
// the revision at changedAt: rolled out once it deployed no earlier, took
// new values after the merge (tookNew), or handled the reconcile
// --reconcile requested (which reads the values as they are), is ready at
// its generation and its workloads are rolled out; failed when it failed
// since.
func (w *Waiter) judgeNewValues(ctx context.Context, d Deployment, hr *unstructured.Unstructured, last deployed, changedAt time.Time, tookNew bool) (Deployment, error) {
	self := object{resource: helmReleases, kind: KindHelmRelease, namespace: hr.GetNamespace(), name: hr.GetName()}
	ready := condition(hr, "Ready")
	current := num(hr, "status", "observedGeneration") >= hr.GetGeneration()
	requestedAt, requested := w.requested[self.String()]
	handled := requested && str(hr, "status", "lastHandledReconcileAt") == requestedAt
	if !last.at.Before(changedAt) || handled || tookNew {
		if !current || ready.status != conditionTrue {
			return d.set(StateProgressing, "upgraded with the values of %s, the HelmRelease not ready: %s", short(w.revision), orUnknown(ready)), nil
		}
		if _, remote, _ := unstructured.NestedMap(hr.Object, "spec", "kubeConfig"); remote {
			w.warnPoll(fmt.Sprintf("%s deploys to a remote cluster: its workloads are not checked, helm-controller's wait vouches for them", d.id()))
			return d.set(StateRolledOut, ""), nil
		}
		return w.judgeWorkloads(ctx, d, last.namespace, last.release)
	}
	if ready.status == conditionFalse && helmReleaseFailures[ready.reason] && !ready.since.Before(changedAt) {
		return d.set(StateFailed, "%s with the values of %s: %s", ready.reason, short(w.revision), ready.message), nil
	}
	// Only now does a reconcile request read the values of the revision.
	d.self = &self
	return d.set(StateProgressing, "last deployed %s, before %s wrote %s with %s at %s: waiting for helm-controller (its interval, or --reconcile); values that render the same never upgrade", formatTime(last.at), d.Follows, d.Source, short(w.revision), formatTime(changedAt)), nil
}

// kustomizeManager is the field manager kustomize-controller applies as.
const kustomizeManager = "kustomize-controller"

// inventoryID is how a Kustomization's status.inventory names an object.
func inventoryID(namespace, name, group, kind string) string {
	return namespace + "_" + name + "_" + group + "_" + kind
}

// appliedAt is when kustomize-controller last changed the object: it
// writes only what differs from the repository, so a no-op apply leaves
// the time as it was. Zero when it never wrote the object.
func appliedAt(u *unstructured.Unstructured) time.Time {
	var t time.Time
	for _, f := range u.GetManagedFields() {
		if f.Manager == kustomizeManager && f.Subresource == "" && f.Time != nil && f.Time.After(t) {
			t = f.Time.Time
		}
	}
	return t
}

// appliedChange is what a Kustomization that reads the repository changed
// for a HelmRelease since the pull request merged.
type appliedChange struct {
	// spec: the HelmRelease itself, a new generation helm-controller
	// reconciles with the values as they are.
	spec bool
	// objects are its valuesFrom ConfigMaps and Secrets and its chartRef
	// source, as "<Kind> <namespace>/<name>"; at the newest change of them.
	objects []string
	at      time.Time
	// by is the Kustomization that applied the change.
	by string
}

// appliedChanges reads what the Kustomizations (applied: inventory ID to
// the Kustomization) changed for a HelmRelease since the merge.
func (w *Waiter) appliedChanges(ctx context.Context, hr *unstructured.Unstructured, applied map[string]string) (appliedChange, error) {
	var c appliedChange
	if w.mergedAt.IsZero() {
		return c, nil
	}
	if by := applied[inventoryID(hr.GetNamespace(), hr.GetName(), helmReleases.Group, KindHelmRelease)]; by != "" && !appliedAt(hr).Before(w.mergedAt) {
		c.spec, c.by = true, by
	}
	type ref struct {
		o     object
		group string
	}
	var refs []ref
	list, _, _ := unstructured.NestedSlice(hr.Object, "spec", "valuesFrom")
	for _, item := range list {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		switch m["kind"] {
		case KindConfigMap:
			refs = append(refs, ref{object{resource: configMaps, kind: KindConfigMap, namespace: hr.GetNamespace(), name: name}, ""})
		case kindSecret:
			refs = append(refs, ref{object{resource: secrets, kind: kindSecret, namespace: hr.GetNamespace(), name: name}, ""})
		}
	}
	if kind := str(hr, "spec", "chartRef", "kind"); kind == kindOCIRepository || kind == kindHelmChart {
		ns := str(hr, "spec", "chartRef", "namespace")
		if ns == "" {
			ns = hr.GetNamespace()
		}
		resource := ociRepositories
		if kind == kindHelmChart {
			resource = helmCharts
		}
		refs = append(refs, ref{object{resource: resource, kind: kind, namespace: ns, name: str(hr, "spec", "chartRef", "name")}, groupSource})
	}
	for _, r := range refs {
		by := applied[inventoryID(r.o.namespace, r.o.name, r.group, r.o.kind)]
		if by == "" {
			continue
		}
		u, err := w.client.Resource(r.o.resource).Namespace(r.o.namespace).Get(ctx, r.o.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if apierrors.IsForbidden(err) {
			w.warnPoll(fmt.Sprintf("%s: reading %s is forbidden, so whether %s changed it is not told", hrID(hr), r.o, by))
			continue
		}
		if err != nil {
			return c, w.clusterErr("reading "+r.o.String(), err)
		}
		if at := appliedAt(u); !at.Before(w.mergedAt) {
			c.objects = append(c.objects, r.o.String())
			c.at = maxTime(c.at, at)
			if c.by == "" {
				c.by = by
			}
		}
	}
	return c, nil
}

func hrID(hr *unstructured.Unstructured) string {
	return KindHelmRelease + " " + hr.GetNamespace() + "/" + hr.GetName()
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// judgeAppliedHelmRelease judges a HelmRelease no rendered values of a
// changed app feed: followed when a Kustomization that reads the
// repository changed it, its values or its chart source with the revision,
// or --helmrelease names it. A new generation is rolled out once
// helm-controller observed it, Ready, its workloads rolled out; changed
// values or a changed chart source once it deployed after the change (or
// handled a --reconcile request). A named HelmRelease nothing changed is
// judged by its Ready.
func (w *Waiter) judgeAppliedHelmRelease(ctx context.Context, hr *unstructured.Unstructured, applied map[string]string) (Deployment, bool, error) {
	c, err := w.appliedChanges(ctx, hr, applied)
	if err != nil {
		return Deployment{}, false, err
	}
	if !c.spec && len(c.objects) == 0 && !w.named(hr.GetNamespace(), hr.GetName()) {
		return Deployment{}, false, nil
	}
	d := Deployment{
		Kind: KindHelmRelease, Namespace: hr.GetNamespace(), Name: hr.GetName(),
		Source: strings.Join(c.objects, ", "), Follows: c.by, Workloads: []Workload{},
	}
	d, last := lastDeploy(d, hr)
	if suspended, _, _ := unstructured.NestedBool(hr.Object, "spec", "suspend"); suspended {
		return d.set(StateSuspended, "the HelmRelease is suspended: flux resume helmrelease -n %s %s", d.Namespace, d.Name), true, nil
	}
	if c.spec || len(c.objects) == 0 {
		d, err := w.judgeNamedHelmRelease(ctx, d, hr, last.namespace, last.release)
		return d, true, err
	}
	d, err = w.judgeNewValues(ctx, d, hr, last, c.at, false)
	return d, true, err
}

// konfigure-operator re-labels what it renders with every revision it
// applies, so the rendered ConfigMap's last write is when it last rendered,
// not when the values last changed: a deploy before it may well run them.
// What a deployment applied tells instead: a deploy after the merge that
// took values other than the deploy before it read the change, which
// renders no earlier than the merge. helm-controller keeps few deploys in
// status.history, so a chart that rolls often evicts that one: when every
// kept deploy is after the merge, the change ran no later than they do.
// (konfigure-operator rendering an older revision after the merge is not
// told apart: its fetch interval.)

// helmReleaseTookNewValues says whether a HelmRelease deployed, after the
// merge, values (status.history's configDigest) other than its previous
// deploy's, or deployed only after the merge as far as its history keeps.
func (w *Waiter) helmReleaseTookNewValues(hr *unstructured.Unstructured) bool {
	if w.mergedAt.IsZero() {
		return false
	}
	history, _, _ := unstructured.NestedSlice(hr.Object, "status", "history")
	for i := range history {
		h, _ := history[i].(map[string]any)
		at, _ := h["lastDeployed"].(string)
		deployed, err := time.Parse(time.RFC3339, at)
		if err != nil || deployed.Before(w.mergedAt) {
			return false
		}
		if i+1 == len(history) {
			return true
		}
		previous, _ := history[i+1].(map[string]any)
		if digest, _ := h["configDigest"].(string); digest != "" && digest != previous["configDigest"] {
			return true
		}
	}
	return false
}

// chartNamespace is where app-operator creates the Chart of an App
// deployed into the management cluster.
const chartNamespace = "giantswarm"

// appTookNewValues says whether an App deployed into the management cluster
// deployed, after the merge, values other than before: app-operator writes
// the values it merges for the App's Chart only when they change.
func (w *Waiter) appTookNewValues(ctx context.Context, app *unstructured.Unstructured, lastDeployed time.Time) bool {
	if inCluster, _, _ := unstructured.NestedBool(app.Object, "spec", "kubeConfig", "inCluster"); !inCluster || w.mergedAt.IsZero() {
		return false
	}
	chart, err := w.client.Resource(charts).Namespace(chartNamespace).Get(ctx, app.GetName(), metav1.GetOptions{})
	if err != nil {
		return false
	}
	name, namespace := str(chart, "spec", "config", "configMap", "name"), str(chart, "spec", "config", "configMap", "namespace")
	if name == "" {
		return false
	}
	merged, err := w.client.Resource(configMaps).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}
	changed := lastWrite(merged)
	return !changed.Before(w.mergedAt) && !lastDeployed.Before(changed)
}

// lastWrite is the newest time a manager wrote the object.
func lastWrite(u *unstructured.Unstructured) time.Time {
	var t time.Time
	for _, f := range u.GetManagedFields() {
		if f.Time != nil && f.Time.After(t) {
			t = f.Time.Time
		}
	}
	return t
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format(time.RFC3339)
}

// AffectedApps reads the apps a configuration change touches on an
// installation from the paths of giantswarm-configs and shared-configs:
// installations/<installation>/apps/<app>/ and default/apps/<app>/ change
// <app>; the rest of installations/<installation>/, default/, include/
// and stages/ changes values beyond one app's (global); another
// installation's files and the repository's own (README, workflows) change
// nothing here.
func AffectedApps(paths []string, installation string) (apps map[string]bool, global []string) {
	apps = map[string]bool{}
	for _, p := range paths {
		parts := strings.Split(p, "/")
		var rest []string
		switch {
		case parts[0] == "installations" && len(parts) > 2:
			if parts[1] != installation {
				continue
			}
			rest = parts[2:]
		case slices.Contains([]string{"default", "include", "stages"}, parts[0]) && len(parts) > 1:
			rest = parts[1:]
		default:
			continue
		}
		if len(rest) > 2 && rest[0] == "apps" {
			apps[rest[1]] = true
			continue
		}
		global = append(global, p)
	}
	return apps, global
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func ids(list []Deployment) string {
	names := make([]string, 0, len(list))
	for _, d := range list {
		names = append(names, d.id())
	}
	return strings.Join(names, ", ")
}
