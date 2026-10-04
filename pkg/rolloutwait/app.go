package rolloutwait

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// apps returns the App CRs on the cluster that deploy one of the charts,
// judged against the version. app-operator pins the version in
// spec.version; the App reports the deployed one in status.version and
// chart-operator's outcome in status.release.status.
func (w *Waiter) apps(ctx context.Context) ([]Deployment, error) {
	items, err := w.list(ctx, apps, "")
	if err != nil {
		return nil, err
	}
	var list []Deployment
	for i := range items {
		app := &items[i]
		chart := str(app, "spec", "name")
		if !w.charts[chart] {
			continue
		}
		d, err := w.judgeApp(ctx, app, chart)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, nil
}

func (w *Waiter) judgeApp(ctx context.Context, app *unstructured.Unstructured, chart string) (Deployment, error) {
	pinned := bare(str(app, "spec", "version"))
	d := Deployment{
		Kind:      KindApp,
		Namespace: app.GetNamespace(),
		Name:      app.GetName(),
		Chart:     chart,
		Source:    "catalog " + str(app, "spec", "catalog"),
		Follows:   "version " + pinned,
		Workloads: []Workload{},
	}
	status, reason := str(app, "status", "release", "status"), str(app, "status", "release", "reason")
	if status == statusDeployed {
		d.RunningVersion = bare(str(app, "status", "version"))
	}
	if atLeast(d.RunningVersion, w.version) {
		if inCluster, _, _ := unstructured.NestedBool(app.Object, "spec", "kubeConfig", "inCluster"); !inCluster {
			w.warnOnce(d.id() + " deploys to a workload cluster: its workloads are not checked")
			return d.set(StateRolledOut, ""), nil
		}
		return w.judgeWorkloads(ctx, d, str(app, "spec", "namespace"), d.Name)
	}
	if !atLeast(pinned, w.version) {
		return d.set(StateNotFollowing, "the App pins %s, older than %s; runs %s", pinned, w.version, orNone(d.RunningVersion)), nil
	}
	if status == "failed" && bare(str(app, "status", "version")) == pinned {
		return d.set(StateFailed, "chart-operator: %s %s", status, reason), nil
	}
	return d.set(StateProgressing, "runs %s; waiting for app-operator to deploy %s (release status %q %s)", orNone(d.RunningVersion), pinned, status, reason), nil
}
