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
// request changed are followed too.

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
)

var (
	gitRepositories = schema.GroupVersionResource{Group: groupSource, Version: "v1", Resource: "gitrepositories"}
	kustomizations  = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	konfigurations  = schema.GroupVersionResource{Group: "konfigure.giantswarm.io", Version: "v1alpha1", Resource: "konfigurations"}
	configMaps      = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
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
// of the sources, and the HelmReleases that take their values from what a
// Konfiguration renders for an app the pull request changed or that
// --helmrelease names.
func (w *Waiter) revisionConsumers(ctx context.Context, sources []object) ([]Deployment, error) {
	ks, err := w.list(ctx, kustomizations, "")
	if err != nil {
		return nil, err
	}
	var list []Deployment
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
	if len(rendered) == 0 {
		return list, nil
	}

	hrs, err := w.list(ctx, helmReleases, "")
	if err != nil {
		return nil, err
	}
	for i := range hrs {
		hr := &hrs[i]
		for _, r := range rendered {
			if r.namespace == hr.GetNamespace() && (r.touched || w.named(hr.GetNamespace(), hr.GetName())) && takesValuesFrom(hr, r.name) {
				d, err := w.judgeValuesHelmRelease(ctx, hr, r)
				if err != nil {
					return nil, err
				}
				list = append(list, d)
				break
			}
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
		if ok && m["name"] == name && (m["kind"] == "ConfigMap" || m["kind"] == "Secret") {
			return true
		}
	}
	return false
}

// judgeValuesHelmRelease judges a HelmRelease whose values a Konfiguration
// renders: rolled out once it deployed after the ConfigMap took the merge
// commit (or handled the reconcile --reconcile requested after it did,
// which reads the values as they are), is ready and its workloads are
// rolled out.
func (w *Waiter) judgeValuesHelmRelease(ctx context.Context, hr *unstructured.Unstructured, r renderedValues) (Deployment, error) {
	d := Deployment{
		Kind: KindHelmRelease, Namespace: hr.GetNamespace(), Name: hr.GetName(),
		Source: "ConfigMap " + r.namespace + "/" + r.name, Follows: r.konfiguration.String(), Workloads: []Workload{},
	}
	self := object{resource: helmReleases, kind: KindHelmRelease, namespace: hr.GetNamespace(), name: hr.GetName()}
	var lastDeployed time.Time
	release, releaseNamespace := "", ""
	if history, _, _ := unstructured.NestedSlice(hr.Object, "status", "history"); len(history) > 0 {
		if h, ok := history[0].(map[string]any); ok && h["status"] == statusDeployed {
			d.Chart, _ = h["chartName"].(string)
			v, _ := h["chartVersion"].(string)
			d.RunningVersion = bare(v)
			release, _ = h["name"].(string)
			releaseNamespace, _ = h["namespace"].(string)
			at, _ := h["lastDeployed"].(string)
			lastDeployed, _ = time.Parse(time.RFC3339, at)
		}
	}
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
	renderedAt := lastWrite(cm)

	ready := condition(hr, "Ready")
	current := num(hr, "status", "observedGeneration") >= hr.GetGeneration()
	requestedAt, requested := w.requested[self.String()]
	handled := requested && str(hr, "status", "lastHandledReconcileAt") == requestedAt
	if !lastDeployed.Before(renderedAt) || handled {
		if !current || ready.status != conditionTrue {
			return d.set(StateProgressing, "upgraded with the values of %s, the HelmRelease not ready: %s", short(w.revision), orUnknown(ready)), nil
		}
		if _, remote, _ := unstructured.NestedMap(hr.Object, "spec", "kubeConfig"); remote {
			w.warnOnce(fmt.Sprintf("%s deploys to a remote cluster: its workloads are not checked, helm-controller's wait vouches for them", d.id()))
			return d.set(StateRolledOut, ""), nil
		}
		return w.judgeWorkloads(ctx, d, releaseNamespace, release)
	}
	if ready.status == conditionFalse && helmReleaseFailures[ready.reason] && !ready.since.Before(renderedAt) {
		return d.set(StateFailed, "%s with the values of %s: %s", ready.reason, short(w.revision), ready.message), nil
	}
	// Only now does a reconcile request read the values of the revision.
	d.self = &self
	return d.set(StateProgressing, "last deployed %s, before %s rendered the values of %s at %s: waiting for helm-controller (its interval, or --reconcile); values that render the same never upgrade", formatTime(lastDeployed), r.konfiguration, short(w.revision), formatTime(renderedAt)), nil
}

// lastWrite is the newest time a manager wrote the object: when
// konfigure-operator last changed what it rendered.
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
