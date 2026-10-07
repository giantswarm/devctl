package rolloutwait

import (
	"context"
	"fmt"
	"path"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The Ready reasons of a HelmRelease whose last attempt failed: the install
// or upgrade failed, its tests failed, or the failure was remediated.
var helmReleaseFailures = map[string]bool{
	"InstallFailed":      true,
	"UpgradeFailed":      true,
	"TestFailed":         true,
	"RollbackSucceeded":  true,
	"UninstallSucceeded": true,
}

// source is a HelmRelease's chart source: the object, the chart it serves
// and how it selects the version.
type source struct {
	object object
	// exists is false for the HelmChart of a spec.chart that helm-controller
	// has not created yet.
	exists  bool
	chart   string
	follows string
	// admits reports whether the selector lets the version in.
	admits func(version string) (bool, error)
	// ready is the source's Ready condition; not found for a HelmChart that
	// helm-controller has not created yet.
	ready cond
}

// helmReleases returns the HelmReleases on the cluster that deploy one of
// the charts or that --helmrelease names, judged against the version.
func (w *Waiter) helmReleases(ctx context.Context) ([]Deployment, error) {
	sources := map[string]*unstructured.Unstructured{}
	for _, r := range []struct {
		kind string
		list func() ([]unstructured.Unstructured, error)
	}{
		{kindOCIRepository, func() ([]unstructured.Unstructured, error) { return w.list(ctx, ociRepositories, "") }},
		{kindHelmChart, func() ([]unstructured.Unstructured, error) { return w.list(ctx, helmCharts, "") }},
	} {
		items, err := r.list()
		if err != nil {
			return nil, err
		}
		for i := range items {
			sources[r.kind+" "+items[i].GetNamespace()+"/"+items[i].GetName()] = &items[i]
		}
	}
	items, err := w.list(ctx, helmReleases, "")
	if err != nil {
		return nil, err
	}
	var list []Deployment
	for i := range items {
		hr := &items[i]
		src, ok := sourceOf(hr, sources)
		named := w.named(hr.GetNamespace(), hr.GetName())
		if !named && (!ok || !w.charts[src.chart]) {
			continue
		}
		if !ok {
			d := Deployment{Kind: KindHelmRelease, Namespace: hr.GetNamespace(), Name: hr.GetName(), Workloads: []Workload{}}
			list = append(list, d.set(StateProgressing, "its chart source does not exist yet"))
			continue
		}
		d, err := w.judgeHelmRelease(ctx, hr, src)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, nil
}

// sourceOf resolves the chart source of a HelmRelease: spec.chartRef to an
// OCIRepository or HelmChart, or the HelmChart helm-controller creates from
// spec.chart. A chartRef to a source that does not exist names no chart.
func sourceOf(hr *unstructured.Unstructured, sources map[string]*unstructured.Unstructured) (source, bool) {
	if kind := str(hr, "spec", "chartRef", "kind"); kind != "" {
		ns := str(hr, "spec", "chartRef", "namespace")
		if ns == "" {
			ns = hr.GetNamespace()
		}
		o := object{kind: kind, namespace: ns, name: str(hr, "spec", "chartRef", "name")}
		u, found := sources[o.String()]
		if !found {
			return source{}, false
		}
		switch kind {
		case kindOCIRepository:
			o.resource = ociRepositories
			return ociSource(o, u), true
		case kindHelmChart:
			o.resource = helmCharts
			return rangeSource(o, true, str(u, "spec", "chart"), str(u, "spec", "version"), condition(u, "Ready")), true
		}
		return source{}, false
	}
	chart := str(hr, "spec", "chart", "spec", "chart")
	if chart == "" {
		return source{}, false
	}
	ns := str(hr, "spec", "chart", "spec", "sourceRef", "namespace")
	if ns == "" {
		ns = hr.GetNamespace()
	}
	o := object{resource: helmCharts, kind: kindHelmChart, namespace: ns, name: hr.GetNamespace() + "-" + hr.GetName()}
	ready := cond{}
	u, exists := sources[o.String()]
	if exists {
		ready = condition(u, "Ready")
	}
	// A chart from a GitRepository or Bucket is a path; its chart is the
	// directory.
	return rangeSource(o, exists, path.Base(chart), str(hr, "spec", "chart", "spec", "version"), ready), true
}

// ociSource reads the chart from the repository URL (its last path
// element, whatever the registry: the Aliyun mirror carries the same
// path) and the selector from spec.ref.
func ociSource(o object, u *unstructured.Unstructured) source {
	s := source{object: o, exists: true, chart: path.Base(str(u, "spec", "url")), ready: condition(u, "Ready")}
	switch semverRange, tag, digest := str(u, "spec", "ref", "semver"), str(u, "spec", "ref", "tag"), str(u, "spec", "ref", "digest"); {
	case digest != "":
		s.follows = "digest " + digest
		s.admits = func(string) (bool, error) { return false, nil }
	case semverRange != "":
		s.follows = "semver " + semverRange
		s.admits = func(v string) (bool, error) { return allows(semverRange, v) }
	default:
		if tag == "" {
			tag = "latest"
		}
		s.follows = "tag " + tag
		s.admits = func(v string) (bool, error) { return bare(tag) == bare(v), nil }
	}
	return s
}

func rangeSource(o object, exists bool, chart, versionRange string, ready cond) source {
	if versionRange == "" {
		versionRange = "*"
	}
	return source{
		object:  o,
		exists:  exists,
		chart:   chart,
		follows: "semver " + versionRange,
		admits:  func(v string) (bool, error) { return allows(versionRange, v) },
		ready:   ready,
	}
}

func (w *Waiter) judgeHelmRelease(ctx context.Context, hr *unstructured.Unstructured, src source) (Deployment, error) {
	d := Deployment{
		Kind:      KindHelmRelease,
		Namespace: hr.GetNamespace(),
		Name:      hr.GetName(),
		Chart:     src.chart,
		Source:    src.object.String(),
		Follows:   src.follows,
		Workloads: []Workload{},
	}
	if src.exists {
		d.source = &src.object
	}
	release, releaseNamespace := "", ""
	if history, _, _ := unstructured.NestedSlice(hr.Object, "status", "history"); len(history) > 0 {
		if h, ok := history[0].(map[string]any); ok && h["status"] == statusDeployed {
			v, _ := h["chartVersion"].(string)
			d.RunningVersion = bare(v)
			release, _ = h["name"].(string)
			releaseNamespace, _ = h["namespace"].(string)
		}
	}
	ready, stalled := condition(hr, "Ready"), condition(hr, "Stalled")
	current := num(hr, "status", "observedGeneration") >= hr.GetGeneration()

	if suspended, _, _ := unstructured.NestedBool(hr.Object, "spec", "suspend"); suspended {
		return d.set(StateSuspended, "the HelmRelease is suspended: flux resume helmrelease -n %s %s", d.Namespace, d.Name), nil
	}
	if atLeast(d.RunningVersion, w.version) {
		if !current || ready.status != conditionTrue {
			return d.set(StateProgressing, "%s deployed, the HelmRelease not ready: %s", d.RunningVersion, orUnknown(ready)), nil
		}
		if _, remote, _ := unstructured.NestedMap(hr.Object, "spec", "kubeConfig"); remote {
			w.warnOnce(fmt.Sprintf("%s deploys to a remote cluster: its workloads are not checked, helm-controller's wait vouches for them", d.id()))
			return d.set(StateRolledOut, ""), nil
		}
		return w.judgeWorkloads(ctx, d, releaseNamespace, release)
	}

	admitted, err := src.admits(w.version)
	if err != nil {
		return d, fmt.Errorf("%s: reading its version selector %q: %w", d.id(), src.follows, err)
	}
	if !admitted {
		return d.set(StateNotFollowing, "%s follows %s, which excludes %s; runs %s", d.Source, src.follows, w.version, orNone(d.RunningVersion)), nil
	}
	if bare(str(hr, "status", "lastAttemptedRevision")) == w.version && (stalled.status == conditionTrue || (ready.status == conditionFalse && helmReleaseFailures[ready.reason])) {
		return d.set(StateFailed, "%s %s: %s", ready.reason, w.version, ready.message), nil
	}
	if src.ready.found && src.ready.status != conditionTrue {
		return d.set(StateProgressing, "runs %s; %s not ready: %s", orNone(d.RunningVersion), d.Source, src.ready.message), nil
	}
	return d.set(StateProgressing, "runs %s; waiting for Flux to deploy %s (the HelmRelease: %s)", orNone(d.RunningVersion), w.version, orUnknown(ready)), nil
}

// judgeWorkloads finishes a deployment whose version is deployed: rolled
// out once every workload of its Helm release is.
func (w *Waiter) judgeWorkloads(ctx context.Context, d Deployment, namespace, release string) (Deployment, error) {
	list, failed, err := w.workloads(ctx, namespace, release)
	if err != nil {
		return d, err
	}
	d.Workloads = list
	if failed != "" {
		return d.set(StateFailed, "%s deployed, a workload failed: %s", d.RunningVersion, failed), nil
	}
	if p := pending(list); p != "" {
		return d.set(StateProgressing, "%s deployed, rolling out: %s", d.RunningVersion, p), nil
	}
	if w.revision == "" && d.RunningVersion != w.version {
		w.warnOnce(fmt.Sprintf("%s runs %s, newer than %s", d.id(), d.RunningVersion, w.version))
	}
	return d.set(StateRolledOut, ""), nil
}

func (d Deployment) set(state, format string, args ...any) Deployment {
	d.State, d.Message = state, fmt.Sprintf(format, args...)
	return d
}

func (d Deployment) id() string { return d.Kind + " " + d.Namespace + "/" + d.Name }

func orNone(version string) string {
	if version == "" {
		return "nothing yet"
	}
	return version
}

func orUnknown(c cond) string {
	if !c.found {
		return "no Ready condition yet"
	}
	return strings.TrimSpace(c.reason + " " + c.message)
}
