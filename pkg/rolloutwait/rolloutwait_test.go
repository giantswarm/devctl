package rolloutwait

import (
	"context"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

const chart = "giantswarm-platform-manager"

func obj(apiVersion, kind, namespace, name string, fields map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": kind}}
	for k, v := range fields {
		u.Object[k] = v
	}
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetGeneration(1)
	return u
}

func ready(status, reason, message string) []any {
	return []any{map[string]any{"type": "Ready", "status": status, "reason": reason, "message": message}}
}

func ociRepository(ref map[string]any) *unstructured.Unstructured {
	return obj("source.toolkit.fluxcd.io/v1", "OCIRepository", "flux-giantswarm", chart, map[string]any{
		"spec":   map[string]any{"url": "oci://gsoci.azurecr.io/charts/giantswarm/" + chart, "ref": ref},
		"status": map[string]any{"conditions": ready("True", "Succeeded", "")},
	})
}

// helmRelease is a HelmRelease of the chart from the OCIRepository whose
// last deployed chart version is running.
func helmRelease(running string, status map[string]any) *unstructured.Unstructured {
	s := map[string]any{"observedGeneration": int64(1), "conditions": ready("True", "UpgradeSucceeded", "Helm upgrade succeeded")}
	if running != "" {
		s["history"] = []any{map[string]any{"chartName": chart, "chartVersion": running + "+6a72bd1e624f", "status": "deployed", "name": chart, "namespace": "agent-platform"}}
	}
	for k, v := range status {
		s[k] = v
	}
	return obj("helm.toolkit.fluxcd.io/v2", "HelmRelease", "flux-giantswarm", chart, map[string]any{
		"spec":   map[string]any{"chartRef": map[string]any{"kind": "OCIRepository", "name": chart, "namespace": "flux-giantswarm"}},
		"status": s,
	})
}

// deployment is a Deployment of the Helm release with updated of 2
// replicas updated and available.
func deployment(updated int64, conditions ...any) *unstructured.Unstructured {
	u := obj("apps/v1", "Deployment", "agent-platform", chart, map[string]any{
		"spec":   map[string]any{"replicas": int64(2)},
		"status": map[string]any{"observedGeneration": int64(1), "replicas": int64(2), "updatedReplicas": updated, "availableReplicas": updated, "conditions": conditions},
	})
	u.SetAnnotations(map[string]string{annotationReleaseName: chart, annotationReleaseNamespace: "agent-platform"})
	return u
}

func app(namespace, name, pinned, deployed, releaseStatus string, inCluster bool) *unstructured.Unstructured {
	return obj("application.giantswarm.io/v1alpha1", "App", namespace, name, map[string]any{
		"spec":   map[string]any{"name": chart, "version": pinned, "catalog": "control-plane-catalog", "namespace": "agent-platform", "kubeConfig": map[string]any{"inCluster": inCluster}},
		"status": map[string]any{"version": deployed, "release": map[string]any{"status": releaseStatus}},
	})
}

type run struct {
	client   *dynamicfake.FakeDynamicClient
	result   Result
	warnings []string
	err      error
}

func newClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), ListKinds(), objects...)
}

func wait(t *testing.T, client *dynamicfake.FakeDynamicClient, reconcile bool, options ...func(*Config)) run {
	t.Helper()
	r := run{client: client, result: NewResult("myinstallation", ContextPrefix+"myinstallation")}
	config := Config{
		Installation: "myinstallation",
		KubeContext:  ContextPrefix + "myinstallation",
		Version:      "v0.48.1",
		Charts:       []string{chart},
		Client:       client,
		Timeout:      time.Minute,
		Reconcile:    reconcile,
		Clock:        agentcli.NewClock(0.0001, nil),
		Warn:         func(m string) { r.warnings = append(r.warnings, m) },
	}
	for _, o := range options {
		o(&config)
	}
	w, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	r.err = w.Wait(context.Background(), &r.result)
	return r
}

func wantExit(t *testing.T, r run, code int, reason string) {
	t.Helper()
	if got := agentcli.Exit(r.err); got != code {
		t.Fatalf("want exit %d, got %d (%v)", code, got, r.err)
	}
	if reason != "" && (r.err == nil || !strings.Contains(r.err.Error(), reason)) {
		t.Errorf("reason %v does not contain %q", r.err, reason)
	}
}

func TestHelmReleaseRolledOut(t *testing.T) {
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0 <1.0.0"}), helmRelease("0.48.1", nil), deployment(2)), false)
	wantExit(t, r, agentcli.ExitOK, "")
	if len(r.result.Deployments) != 1 {
		t.Fatalf("deployments: %+v", r.result.Deployments)
	}
	d := r.result.Deployments[0]
	if d.State != StateRolledOut || d.RunningVersion != "0.48.1" || d.Follows != "semver >=0.17.0 <1.0.0" || d.Source != "OCIRepository flux-giantswarm/"+chart {
		t.Errorf("deployment: %+v", d)
	}
	if len(d.Workloads) != 1 || !d.Workloads[0].Ready {
		t.Errorf("workloads: %+v", d.Workloads)
	}
	if r.result.Version != "0.48.1" || len(r.result.Charts) != 1 {
		t.Errorf("result: %+v", r.result)
	}
}

// A chart that renders its workloads into another namespace than the
// release's (kagent's controller in kagent, the release in agent-platform):
// the workload is found by Helm's annotations and its rollout waited for.
func TestWorkloadInAnotherNamespaceIsJudged(t *testing.T) {
	elsewhere := deployment(1)
	elsewhere.SetNamespace("kagent")
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), elsewhere), false)
	wantExit(t, r, agentcli.ExitTimeout, "Deployment kagent/"+chart)
	if w := r.result.Deployments[0].Workloads; len(w) != 1 || w[0].Namespace != "kagent" || w[0].Ready {
		t.Errorf("workloads: %+v", w)
	}
}

func TestNewerVersionCountsWithAWarning(t *testing.T) {
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.49.0", nil), deployment(2)), false)
	wantExit(t, r, agentcli.ExitOK, "")
	if len(r.warnings) != 1 || !strings.Contains(r.warnings[0], "newer than 0.48.1") {
		t.Errorf("warnings: %v", r.warnings)
	}
}

func TestSourceThatExcludesTheVersionIsNotApplicable(t *testing.T) {
	for name, ref := range map[string]map[string]any{
		"semver range": {"semver": "<0.48.0"},
		"pinned tag":   {"tag": "0.47.0"},
		"digest":       {"digest": "sha256:abc"},
	} {
		t.Run(name, func(t *testing.T) {
			r := wait(t, newClient(ociRepository(ref), helmRelease("0.47.0", nil)), false)
			wantExit(t, r, agentcli.ExitNotApplicable, "excludes 0.48.1")
			if r.result.Deployments[0].State != StateNotFollowing {
				t.Errorf("state: %+v", r.result.Deployments[0])
			}
		})
	}
}

func TestNotFollowingDeploymentDoesNotBlockTheOthers(t *testing.T) {
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), deployment(2),
		app("org-acme", "acme-"+chart, "0.40.0", "0.40.0", "deployed", false)), false)
	wantExit(t, r, agentcli.ExitOK, "")
	if len(r.result.Deployments) != 2 || r.result.Deployments[1].State != StateNotFollowing {
		t.Fatalf("deployments: %+v", r.result.Deployments)
	}
	if len(r.warnings) != 1 || !strings.Contains(r.warnings[0], "the App pins 0.40.0") {
		t.Errorf("warnings: %v", r.warnings)
	}
}

func TestSuspendedHelmReleaseIsNotApplicable(t *testing.T) {
	hr := helmRelease("0.47.0", nil)
	_ = unstructured.SetNestedField(hr.Object, true, "spec", "suspend")
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), hr), false)
	wantExit(t, r, agentcli.ExitNotApplicable, "suspended")
}

func TestFailedUpgradeOfTheVersionIsExit1(t *testing.T) {
	for name, status := range map[string]map[string]any{
		"stalled": {"lastAttemptedRevision": "0.48.1+6a72bd1e624f", "conditions": append(ready("False", "UpgradeFailed", "context deadline exceeded"),
			map[string]any{"type": "Stalled", "status": "True", "reason": "RetriesExceeded"})},
		"upgrade failed": {"lastAttemptedRevision": "0.48.1+6a72bd1e624f", "conditions": ready("False", "UpgradeFailed", "context deadline exceeded")},
	} {
		t.Run(name, func(t *testing.T) {
			r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.0", status)), false)
			wantExit(t, r, agentcli.ExitRed, "UpgradeFailed 0.48.1: context deadline exceeded")
			if _, verdict := agentcli.Outcome(r.err); verdict != VerdictRolloutFailed {
				t.Errorf("verdict %s", verdict)
			}
		})
	}
}

func TestFailedUpgradeOfAnOlderVersionKeepsWaiting(t *testing.T) {
	status := map[string]any{"lastAttemptedRevision": "0.48.0", "conditions": ready("False", "UpgradeFailed", "older failure")}
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.47.0", status)), false)
	wantExit(t, r, agentcli.ExitTimeout, "waiting for Flux to deploy 0.48.1")
}

func TestWorkloadPastItsDeadlineIsExit1(t *testing.T) {
	stuck := map[string]any{"type": "Progressing", "status": "False", "reason": "ProgressDeadlineExceeded", "message": "ReplicaSet has timed out progressing"}
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), deployment(1, stuck)), false)
	wantExit(t, r, agentcli.ExitRed, "progress deadline exceeded")
}

func TestTimeoutNamesWhatIsMissing(t *testing.T) {
	r := wait(t, newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), deployment(1)), false)
	wantExit(t, r, agentcli.ExitTimeout, "1 of 2 replicas updated")
	if r.result.Deployments[0].State != StateProgressing {
		t.Errorf("state: %+v", r.result.Deployments[0])
	}
}

func TestNothingDeploysTheChart(t *testing.T) {
	r := wait(t, newClient(), false)
	wantExit(t, r, agentcli.ExitNotApplicable, "no HelmRelease or App on myinstallation deploys "+chart)
}

func TestRolloutIsFollowedUntilDone(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), deployment(2))
	polls := 0
	client.PrependReactor("list", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
		polls++
		running := "0.48.0"
		if polls > 2 {
			running = "0.48.1"
		}
		list := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmReleaseList"}}
		list.Items = []unstructured.Unstructured{*helmRelease(running, nil)}
		return true, list, nil
	})
	r := wait(t, client, false)
	wantExit(t, r, agentcli.ExitOK, "")
	if polls != 3 {
		t.Errorf("want 3 polls, got %d", polls)
	}
}

func TestReconcileAnnotatesSourceAndHelmReleaseOnce(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.0", nil))
	r := wait(t, client, true)
	wantExit(t, r, agentcli.ExitTimeout, "")
	patches := map[string]int{}
	for _, a := range client.Actions() {
		if p, ok := a.(k8stesting.PatchAction); ok {
			patches[p.GetResource().Resource]++
			if !strings.Contains(string(p.GetPatch()), "reconcile.fluxcd.io/requestedAt") {
				t.Errorf("patch %s", p.GetPatch())
			}
		}
	}
	if patches["ocirepositories"] != 1 || patches["helmreleases"] != 1 || len(patches) != 2 {
		t.Errorf("patches: %v", patches)
	}
}

func TestAppRolledOut(t *testing.T) {
	r := wait(t, newClient(app("giantswarm", chart, "0.48.1", "0.48.1", "deployed", true), deployment(2)), false)
	wantExit(t, r, agentcli.ExitOK, "")
	d := r.result.Deployments[0]
	if d.Kind != KindApp || d.Source != "catalog control-plane-catalog" || len(d.Workloads) != 1 {
		t.Errorf("deployment: %+v", d)
	}
}

func TestAppIntoAWorkloadClusterWarnsAboutWorkloads(t *testing.T) {
	r := wait(t, newClient(app("org-x", "wc-"+chart, "0.48.1", "0.48.1", "deployed", false)), false)
	wantExit(t, r, agentcli.ExitOK, "")
	if len(r.warnings) != 1 || !strings.Contains(r.warnings[0], "workloads are not checked") {
		t.Errorf("warnings: %v", r.warnings)
	}
}

func TestAppReleaseFailedIsExit1(t *testing.T) {
	a := app("giantswarm", chart, "0.48.1", "0.48.1", "failed", true)
	_ = unstructured.SetNestedField(a.Object, "helm upgrade failed", "status", "release", "reason")
	r := wait(t, newClient(a), false)
	wantExit(t, r, agentcli.ExitRed, "helm upgrade failed")
}

func TestHelmReleaseFromChartTemplate(t *testing.T) {
	hr := obj("helm.toolkit.fluxcd.io/v2", "HelmRelease", "flux-giantswarm", chart, map[string]any{
		"spec": map[string]any{"chart": map[string]any{"spec": map[string]any{
			"chart": chart, "version": "0.48.x", "sourceRef": map[string]any{"kind": "HelmRepository", "name": "control-plane-catalog"},
		}}},
		"status": helmRelease("0.48.1", nil).Object["status"],
	})
	r := wait(t, newClient(hr, deployment(2)), false)
	wantExit(t, r, agentcli.ExitOK, "")
	if d := r.result.Deployments[0]; d.Source != "HelmChart flux-giantswarm/flux-giantswarm-"+chart || d.Follows != "semver 0.48.x" {
		t.Errorf("deployment: %+v", d)
	}
}

func TestJudgeStatefulSetAndDaemonSet(t *testing.T) {
	sts := func(status map[string]any, strategy map[string]any) *unstructured.Unstructured {
		status["observedGeneration"] = int64(1)
		return obj("apps/v1", "StatefulSet", "ns", "s", map[string]any{"spec": map[string]any{"replicas": int64(3), "updateStrategy": strategy}, "status": status})
	}
	ds := func(status map[string]any) *unstructured.Unstructured {
		status["observedGeneration"] = int64(1)
		return obj("apps/v1", "DaemonSet", "ns", "d", map[string]any{"spec": map[string]any{}, "status": status})
	}
	cases := []struct {
		name  string
		judge judgement
		ready bool
	}{
		{"sts ready", judgeStatefulSet(sts(map[string]any{"readyReplicas": int64(3), "currentRevision": "a", "updateRevision": "a"}, nil)), true},
		{"sts old revision", judgeStatefulSet(sts(map[string]any{"readyReplicas": int64(3), "currentRevision": "a", "updateRevision": "b"}, nil)), false},
		{"sts not ready", judgeStatefulSet(sts(map[string]any{"readyReplicas": int64(2)}, nil)), false},
		{"sts partition", judgeStatefulSet(sts(map[string]any{"readyReplicas": int64(3), "updatedReplicas": int64(1)}, map[string]any{"rollingUpdate": map[string]any{"partition": int64(2)}})), true},
		{"sts on delete", judgeStatefulSet(sts(map[string]any{}, map[string]any{"type": "OnDelete"})), true},
		{"ds ready", judgeDaemonSet(ds(map[string]any{"desiredNumberScheduled": int64(4), "updatedNumberScheduled": int64(4), "numberAvailable": int64(4)})), true},
		{"ds updating", judgeDaemonSet(ds(map[string]any{"desiredNumberScheduled": int64(4), "updatedNumberScheduled": int64(3), "numberAvailable": int64(4)})), false},
	}
	for _, tc := range cases {
		if tc.judge.ready != tc.ready {
			t.Errorf("%s: want ready %v, got %+v", tc.name, tc.ready, tc.judge)
		}
	}
}

func TestChartName(t *testing.T) {
	if got := ChartName("gsoci.azurecr.io/charts/giantswarm/agent-platform-connectivity:4.84.2"); got != "agent-platform-connectivity" {
		t.Errorf("got %q", got)
	}
}

func TestUnauthorizedClusterIsExit8(t *testing.T) {
	client := newClient()
	client.PrependReactor("list", "ocirepositories", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewUnauthorized("certificate expired")
	})
	r := wait(t, client, false)
	wantExit(t, r, agentcli.ExitAuthRequired, "tsh kube login myinstallation")
}

// companion is a second chart released off the same tag and pinned to the
// release's exact version (giantswarm/agent-platform's connectivity chart),
// deployed by a HelmRelease of its own.
const companion = chart + "-connectivity"

func companionSource() *unstructured.Unstructured {
	u := ociRepository(map[string]any{"semver": "0.48.1"})
	u.SetName(companion)
	_ = unstructured.SetNestedField(u.Object, "oci://gsoci.azurecr.io/charts/giantswarm/"+companion, "spec", "url")
	return u
}

func companionRelease(running string) *unstructured.Unstructured {
	u := helmRelease(running, nil)
	u.SetName(companion)
	_ = unstructured.SetNestedField(u.Object, companion, "spec", "chartRef", "name")
	// Its Helm release is its own: the fixture's Deployment is not one of
	// its workloads.
	history, _, _ := unstructured.NestedSlice(u.Object, "status", "history")
	if len(history) > 0 {
		history[0].(map[string]any)["name"] = companion
		_ = unstructured.SetNestedSlice(u.Object, history, "status", "history")
	}
	return u
}

func withCharts(charts ...string) func(*Config) {
	return func(c *Config) { c.Charts = charts }
}

// The release's second HelmRelease upgrades minutes after the first: the
// wait is rolled out only once both run the version.
func TestSecondHelmReleaseLaggingKeepsTheWaitOpen(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), companionSource(), deployment(2))
	polls := 0
	client.PrependReactor("list", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
		polls++
		lagging := "0.48.0"
		if polls > 2 {
			lagging = "0.48.1"
		}
		list := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmReleaseList"}}
		list.Items = []unstructured.Unstructured{*helmRelease("0.48.1", nil), *companionRelease(lagging)}
		return true, list, nil
	})
	r := wait(t, client, false, withCharts(chart, companion))
	wantExit(t, r, agentcli.ExitOK, "")
	if polls != 3 {
		t.Errorf("want 3 polls, got %d", polls)
	}
	if len(r.result.Deployments) != 2 {
		t.Fatalf("deployments: %+v", r.result.Deployments)
	}
	for _, d := range r.result.Deployments {
		if d.State != StateRolledOut || d.RunningVersion != "0.48.1" {
			t.Errorf("deployment: %+v", d)
		}
	}
}

func TestSecondHelmReleaseNeverUpgradingIsATimeoutNamingIt(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), deployment(2), companionSource(), companionRelease("0.48.0"))
	r := wait(t, client, false, withCharts(chart, companion))
	wantExit(t, r, agentcli.ExitTimeout, "HelmRelease flux-giantswarm/"+companion+": runs 0.48.0")
	states := map[string]string{}
	for _, d := range r.result.Deployments {
		states[d.Name] = d.State
	}
	if states[chart] != StateRolledOut || states[companion] != StateProgressing {
		t.Errorf("states: %v", states)
	}
}

// --helmrelease adds a HelmRelease the release's charts do not name.
func TestNamedHelmReleaseIsWaitedFor(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), deployment(2), companionSource(), companionRelease("0.48.0"))
	for _, name := range []string{companion, "flux-giantswarm/" + companion} {
		r := wait(t, client, false, func(c *Config) { c.HelmReleases = []string{name} })
		wantExit(t, r, agentcli.ExitTimeout, "HelmRelease flux-giantswarm/"+companion)
		if len(r.result.Deployments) != 2 {
			t.Errorf("%s: deployments: %+v", name, r.result.Deployments)
		}
	}
}

func TestNamedHelmReleaseMissingIsNotApplicable(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), helmRelease("0.48.1", nil), deployment(2))
	r := wait(t, client, false, func(c *Config) { c.HelmReleases = []string{"other/" + companion} })
	wantExit(t, r, agentcli.ExitNotApplicable, "no HelmRelease other/"+companion+" on myinstallation")
}

func TestNamedHelmReleaseMalformedIsUsage(t *testing.T) {
	for _, name := range []string{"", "/x", "x/", "a/b/c"} {
		_, err := New(Config{Client: newClient(), Charts: []string{chart}, HelmReleases: []string{name}})
		if agentcli.Exit(err) != agentcli.ExitUsage {
			t.Errorf("%q: want exit %d, got %v", name, agentcli.ExitUsage, err)
		}
	}
}
