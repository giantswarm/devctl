package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

const componentName = "agent-platform-connectivity"

// componentCommand exercises flag parsing and the real rollout engine. Its
// release opener refuses access: the component version is not a meta-chart tag.
func componentCommand(t *testing.T, args []string, objects ...runtime.Object) (rolloutwait.Document, error, int, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), objects...)
	var stdout, stderr bytes.Buffer
	releaseCalls := 0
	f := &flag{}
	r := &runner{
		flag: f, stdout: &stdout, stderr: &stderr,
		gate: func(bool) error { return nil },
		openCluster: func(context.Context, laptop.ClusterConfig) (dynamic.Interface, error) {
			return client, nil
		},
		openRelease: func(context.Context, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
			releaseCalls++
			return nil, errors.New("repository release must not be read for a component")
		},
	}
	cmd := &cobra.Command{Use: "wait", RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
	f.Init(cmd)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	var doc rolloutwait.Document
	if jsonErr := json.Unmarshal(stdout.Bytes(), &doc); jsonErr != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", jsonErr, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
	return doc, err, releaseCalls, client
}

func componentObjects(kind, running, ready string, observed, available int64) []runtime.Object {
	component := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease",
		"metadata": map[string]any{"name": componentName, "namespace": "agent-platform", "generation": int64(2)},
		"spec":     map[string]any{"chartRef": map[string]any{"kind": "OCIRepository", "name": componentName}},
		"status": map[string]any{
			"observedGeneration": observed, "lastAttemptedRevision": "1.2.3",
			"conditions": []any{map[string]any{"type": "Ready", "status": ready, "reason": "UpgradeFailed"}},
			"history":    []any{map[string]any{"chartVersion": running + "+digest", "status": "deployed", "name": componentName, "namespace": "agent-platform"}},
		},
	}}
	if kind == rolloutwait.KindApp {
		status := "deployed"
		if ready == "False" {
			status = "failed"
		}
		component = &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "application.giantswarm.io/v1alpha1", "kind": "App",
			"metadata": map[string]any{"name": componentName, "namespace": "agent-platform"},
			"spec":     map[string]any{"name": componentName, "version": "1.2.3", "namespace": "agent-platform", "kubeConfig": map[string]any{"inCluster": true}},
			"status":   map[string]any{"version": running, "release": map[string]any{"status": status}},
		}}
	}
	return []runtime.Object{
		component,
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepository",
			"metadata": map[string]any{"name": componentName, "namespace": "agent-platform"},
			"spec":     map[string]any{"url": "oci://example.invalid/charts/" + componentName, "ref": map[string]any{"semver": ">=1.0.0 <2.0.0"}},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease",
			"metadata": map[string]any{"name": "agent-platform", "namespace": "agent-platform"},
			"spec":     map[string]any{"chart": map[string]any{"spec": map[string]any{"chart": "agent-platform", "version": "0.70.0"}}},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apps/v1", "kind": "Deployment",
			"metadata": map[string]any{"name": componentName, "namespace": "agent-platform", "generation": int64(1), "annotations": map[string]any{
				"meta.helm.sh/release-name": componentName, "meta.helm.sh/release-namespace": "agent-platform",
			}},
			"spec":   map[string]any{"replicas": int64(2)},
			"status": map[string]any{"observedGeneration": int64(1), "replicas": int64(2), "updatedReplicas": available, "availableReplicas": available},
		}},
	}
}

func TestComponentChartWait(t *testing.T) {
	for _, tc := range []struct {
		name, kind, running, ready string
		observed, available        int64
		code                       int
		state                      string
	}{
		{"range-pinned component", rolloutwait.KindHelmRelease, "1.2.3", "True", 2, 2, 0, rolloutwait.StateRolledOut},
		{"ready at an older version", rolloutwait.KindHelmRelease, "1.2.2", "True", 2, 2, 2, rolloutwait.StateProgressing},
		{"target version not ready", rolloutwait.KindHelmRelease, "1.2.3", "False", 2, 2, 2, rolloutwait.StateProgressing},
		{"unobserved generation", rolloutwait.KindHelmRelease, "1.2.3", "True", 1, 2, 2, rolloutwait.StateProgressing},
		{"workload still rolling", rolloutwait.KindHelmRelease, "1.2.3", "True", 2, 1, 2, rolloutwait.StateProgressing},
		{"failed component upgrade", rolloutwait.KindHelmRelease, "1.2.2", "False", 2, 2, 1, rolloutwait.StateFailed},
		{"App component", rolloutwait.KindApp, "1.2.3", "True", 2, 2, 0, rolloutwait.StateRolledOut},
		{"App still on older version", rolloutwait.KindApp, "1.2.2", "True", 2, 2, 2, rolloutwait.StateProgressing},
		{"failed App release", rolloutwait.KindApp, "1.2.3", "False", 2, 2, 1, rolloutwait.StateFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err, releaseCalls, client := componentCommand(t, []string{
				"myinstallation", "giantswarm/agent-platform", "--chart", componentName, "v1.2.3", "--timeout", "1ns",
			}, componentObjects(tc.kind, tc.running, tc.ready, tc.observed, tc.available)...)
			if agentcli.Exit(err) != tc.code || doc.ExitCode != tc.code {
				t.Fatalf("want exit %d, got %d, document %d (%v)", tc.code, agentcli.Exit(err), doc.ExitCode, err)
			}
			if releaseCalls != 0 || doc.Release.Tag != "" || doc.Release.Verdict != agentcli.VerdictNotApplicable || !strings.Contains(doc.Release.Reason, "not checked") {
				t.Errorf("release calls %d, result %+v", releaseCalls, doc.Release)
			}
			if doc.Version != "1.2.3" || len(doc.Charts) != 1 || doc.Charts[0] != componentName || len(doc.Deployments) != 1 {
				t.Fatalf("component result: %+v", doc.Result)
			}
			if d := doc.Deployments[0]; d.Kind != tc.kind || d.Name != componentName || d.State != tc.state {
				t.Errorf("deployment: %+v", d)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "list" {
					t.Errorf("unexpected cluster operation: %s", action.GetVerb())
				}
			}
		})
	}
}

func TestComponentChartVersionFlagAndReconcile(t *testing.T) {
	doc, err, calls, client := componentCommand(t, []string{
		"myinstallation", "giantswarm/agent-platform", "--chart", componentName, "--version", "1.2.3", "--reconcile", "--timeout", "1ns",
	}, componentObjects(rolloutwait.KindHelmRelease, "1.2.2", "True", 2, 2)...)
	if agentcli.Exit(err) != agentcli.ExitTimeout || calls != 0 || doc.Version != "1.2.3" {
		t.Fatalf("document %+v, release calls %d, error %v", doc, calls, err)
	}
	patches := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "patch" {
			patches++
		}
	}
	if patches != 2 {
		t.Errorf("want a reconcile request on the source and HelmRelease, got %d patches", patches)
	}
}

func TestComponentChartUsage(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"myinstallation", "giantswarm/agent-platform"},
		{"myinstallation", "giantswarm/agent-platform", "--chart", componentName},
	} {
		doc, err, calls, client := componentCommand(t, args)
		if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(doc.Reason, "--chart <chart-name> <version>") || calls != 0 || len(client.Actions()) != 0 {
			t.Errorf("args %v: document %+v, release calls %d, error %v", args, doc, calls, err)
		}
	}
	for _, flags := range [][]string{
		{"--pr", "7"}, {"--image", "giantswarm/agent"}, {"--failed-log"},
		{"--chart", "another-component"}, {"--chart", "giantswarm/kagent/helm/kagent"},
	} {
		args := append([]string{"myinstallation", "giantswarm/agent-platform", "v1.2.3", "--chart", componentName}, flags...)
		doc, err, calls, client := componentCommand(t, args)
		if agentcli.Exit(err) != agentcli.ExitUsage || calls != 0 || len(client.Actions()) != 0 {
			t.Errorf("args %v: document %+v, release calls %d, error %v", args, doc, calls, err)
		}
	}
}

func TestComponentChartInvalidName(t *testing.T) {
	for _, chart := range []string{"kagent:1.2.3", "bad name", "kagent@sha256:digest"} {
		doc, err, calls, client := componentCommand(t, []string{"myinstallation", "giantswarm/agent-platform", "v1.2.3", "--chart", chart})
		if agentcli.Exit(err) != agentcli.ExitUsage || !strings.Contains(doc.Reason, "--chart") || calls != 0 || len(client.Actions()) != 0 {
			t.Errorf("chart %q: document %+v, release calls %d, error %v", chart, doc, calls, err)
		}
	}
}

func TestComponentChartMissingDeployment(t *testing.T) {
	doc, err, calls, _ := componentCommand(t, []string{"myinstallation", "giantswarm/agent-platform", "v1.2.3", "--chart", componentName})
	if agentcli.Exit(err) != agentcli.ExitNotApplicable || calls != 0 || !strings.Contains(doc.Reason, componentName) {
		t.Errorf("document %+v, release calls %d, error %v", doc, calls, err)
	}
}

func TestComponentChartPathKeepsReleaseWait(t *testing.T) {
	_, _, calls, _ := componentCommand(t, []string{
		"myinstallation", "giantswarm/kagent-upstream", "v1.2.3", "--chart", "giantswarm/kagent/helm/kagent", "--chart", "giantswarm/kagent/helm/kagent-crds",
	})
	if calls != 1 {
		t.Errorf("want the repository release wait opened for chart paths, got %d calls", calls)
	}
}
