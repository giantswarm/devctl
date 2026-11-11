package rolloutwait

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			w.warnPoll(d.id() + " deploys to a workload cluster: its workloads are not checked")
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

// appTakesValuesFrom says whether an App reads the rendered values: its
// spec.config or spec.userConfig names the ConfigMap or the Secret.
func appTakesValuesFrom(app *unstructured.Unstructured, r renderedValues) bool {
	for _, config := range []string{"config", "userConfig"} {
		for _, kind := range []string{"configMap", "secret"} {
			namespace := str(app, "spec", config, kind, "namespace")
			if namespace == "" {
				namespace = app.GetNamespace()
			}
			if str(app, "spec", config, kind, "name") == r.name && namespace == r.namespace {
				return true
			}
		}
	}
	return false
}

// judgeValuesApp judges an App whose values a Konfiguration renders: rolled
// out once chart-operator deployed its pinned version after the ConfigMap
// took the merge commit, and its workloads are rolled out.
func (w *Waiter) judgeValuesApp(ctx context.Context, app *unstructured.Unstructured, r renderedValues) (Deployment, error) {
	pinned := bare(str(app, "spec", "version"))
	d := Deployment{
		Kind: KindApp, Namespace: app.GetNamespace(), Name: app.GetName(), Chart: str(app, "spec", "name"),
		Source: "ConfigMap " + r.namespace + "/" + r.name, Follows: r.konfiguration.String(), Workloads: []Workload{},
	}
	status, reason := str(app, "status", "release", "status"), str(app, "status", "release", "reason")
	if status == statusDeployed {
		d.RunningVersion = bare(str(app, "status", "version"))
	}
	lastDeployed, _ := time.Parse(time.RFC3339, str(app, "status", "release", "lastDeployed"))

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
	if lastDeployed.Before(renderedAt) && !w.appTookNewValues(ctx, app, lastDeployed) {
		return d.set(StateProgressing, "last deployed %s, before %s rendered the values of %s at %s: waiting for app-operator and chart-operator; values that render the same never upgrade", formatTime(lastDeployed), r.konfiguration, short(w.revision), formatTime(renderedAt)), nil
	}
	if status == "failed" {
		return d.set(StateFailed, "chart-operator: %s %s with the values of %s", status, reason, short(w.revision)), nil
	}
	if status != statusDeployed || d.RunningVersion != pinned {
		return d.set(StateProgressing, "runs %s; waiting for app-operator to deploy %s with the values of %s (release status %q %s)", orNone(d.RunningVersion), pinned, short(w.revision), status, reason), nil
	}
	if inCluster, _, _ := unstructured.NestedBool(app.Object, "spec", "kubeConfig", "inCluster"); !inCluster {
		w.warnPoll(d.id() + " deploys to a workload cluster: its workloads are not checked")
		return d.set(StateRolledOut, ""), nil
	}
	return w.judgeWorkloads(ctx, d, str(app, "spec", "namespace"), d.Name)
}
