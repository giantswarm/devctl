package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

// runComponent parses the real flags, but opens only an in-memory cluster.
// A repository release lookup would need an unrelated meta-chart version.
func runComponent(t *testing.T, client dynamic.Interface, args ...string) (rolloutwait.Document, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	r := &runner{
		flag:   &flag{},
		stdout: &stdout,
		stderr: &stderr,
		gate:   func(bool) error { return nil },
		openCluster: func(_ context.Context, c laptop.ClusterConfig) (dynamic.Interface, error) {
			if client == nil {
				t.Fatal("invalid component call must stop before opening the cluster")
			}
			if c.Installation != "myinstallation" || c.Context != "kind-offline" || c.Kubeconfig != "offline.kubeconfig" || c.Wrap == nil {
				t.Errorf("cluster configuration: %+v", c)
			}
			return client, nil
		},
		openRelease: func(context.Context, string, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
			t.Fatal("component wait must not open repository release sources")
			return nil, nil
		},
	}
	cmd := &cobra.Command{
		RunE:          func(cmd *cobra.Command, args []string) error { return r.run(cmd.Context(), args) },
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	r.flag.Init(cmd)
	cmd.SetArgs(append([]string{"myinstallation", "--context", "kind-offline", "--kubeconfig", "offline.kubeconfig"}, args...))
	err := cmd.ExecuteContext(context.Background())
	var doc rolloutwait.Document
	if jsonErr := json.Unmarshal(stdout.Bytes(), &doc); jsonErr != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", jsonErr, stdout.String())
	}
	if doc.Command != rolloutwait.Command || doc.ExitCode != agentcli.Exit(err) {
		t.Errorf("envelope does not match outcome: %+v; %v", doc.Envelope, err)
	}
	return doc, stderr.String(), err
}

func componentObjects(version, ready, reason string, updated int64) []runtime.Object {
	source := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1", "kind": "OCIRepository",
		"metadata": map[string]any{"namespace": "flux-giantswarm", "name": "kagent"},
		"spec":     map[string]any{"url": "oci://registry.invalid/charts/kagent", "ref": map[string]any{"semver": ">=1.2.0 <1.3.0"}},
	}}
	hr := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease",
		"metadata": map[string]any{"namespace": "flux-giantswarm", "name": "kagent", "generation": int64(2)},
		"spec":     map[string]any{"chartRef": map[string]any{"kind": "OCIRepository", "name": "kagent"}},
		"status": map[string]any{
			"observedGeneration": int64(2), "lastAttemptedRevision": "1.2.4",
			"conditions": []any{map[string]any{"type": "Ready", "status": ready, "reason": reason}},
			"history":    []any{map[string]any{"chartVersion": version, "status": "deployed", "name": "kagent", "namespace": "agent-platform"}},
		},
	}}
	workload := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"namespace": "kagent", "name": "controller", "generation": int64(1), "annotations": map[string]any{
			"meta.helm.sh/release-name": "kagent", "meta.helm.sh/release-namespace": "agent-platform",
		}},
		"spec":   map[string]any{"replicas": int64(2)},
		"status": map[string]any{"observedGeneration": int64(1), "replicas": int64(2), "updatedReplicas": updated, "availableReplicas": updated},
	}}
	return []runtime.Object{source, hr, workload}
}

func TestRunComponentWait(t *testing.T) {
	t.Setenv(agentcli.EnvTimeScale, "0.001")
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds())
	polls := 0
	client.PrependReactor("list", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
		polls++
		version, ready := "1.2.3", "True"
		if polls >= 2 {
			version, ready = "1.2.4+test", "False"
		}
		if polls >= 3 {
			ready = "True"
		}
		hr := componentObjects(version, ready, "Progressing", 0)[1].(*unstructured.Unstructured)
		if polls == 3 {
			_ = unstructured.SetNestedField(hr.Object, int64(1), "status", "observedGeneration")
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*hr}}, nil
	})
	client.PrependReactor("list", "ocirepositories", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*componentObjects("", "", "", 0)[0].(*unstructured.Unstructured)}}, nil
	})
	client.PrependReactor("list", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		updated := int64(1)
		if polls <= 3 || polls >= 5 {
			updated = 2
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*componentObjects("", "", "", updated)[2].(*unstructured.Unstructured)}}, nil
	})
	doc, progress, err := runComponent(t, client, "--chart", "kagent", "--version", "v1.2.4", "--progress", "--timeout", "1m")
	if err != nil {
		t.Fatalf("component wait: %v", err)
	}
	if polls != 5 || !strings.Contains(progress, "1 of 2 replicas updated") || !strings.Contains(progress, "not ready") {
		t.Errorf("want old version, readiness, generation and workload polls; got %d: %s", polls, progress)
	}
	if doc.Verdict != rolloutwait.VerdictRolledOut || doc.Version != "1.2.4" || len(doc.Charts) != 1 || doc.Charts[0] != "kagent" {
		t.Errorf("component result: %+v", doc)
	}
	if len(doc.Deployments) != 1 || doc.Deployments[0].Chart != "kagent" || doc.Deployments[0].RunningVersion != "1.2.4" || len(doc.Deployments[0].Workloads) != 1 || !doc.Deployments[0].Workloads[0].Ready {
		t.Errorf("component deployment: %+v", doc.Deployments)
	}
	if doc.Release.Verdict != agentcli.VerdictNotApplicable || doc.Release.Tag != "" || !strings.Contains(doc.Release.Reason, "skipped") {
		t.Errorf("must not claim a repository release was verified: %+v", doc.Release)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "list" {
			t.Errorf("read-only wait made a %s request", action.GetVerb())
		}
	}
}

func TestRunComponentOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		objects []runtime.Object
		refused bool
		code    int
		reason  string
	}{
		{name: "case 0: already running", objects: componentObjects("1.2.4", "True", "Succeeded", 2), code: agentcli.ExitOK},
		{name: "case 1: old version is not success", objects: componentObjects("1.2.3", "True", "Succeeded", 2), code: agentcli.ExitTimeout, reason: "waiting for Flux to deploy 1.2.4"},
		{name: "case 2: workload is not ready", objects: componentObjects("1.2.4", "True", "Succeeded", 1), code: agentcli.ExitTimeout, reason: "1 of 2 replicas updated"},
		{name: "case 3: settled upgrade failed", objects: componentObjects("1.2.3", "False", "UpgradeFailed", 2), code: agentcli.ExitRed, reason: "UpgradeFailed"},
		{name: "case 4: chart not installed", code: agentcli.ExitNotApplicable, reason: "no HelmRelease or App"},
		{name: "case 5: cluster refuses access", refused: true, code: agentcli.ExitAuthRequired, reason: "sign in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), tc.objects...)
			if tc.refused {
				client.PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewUnauthorized("offline refusal")
				})
			}
			// Less than the first poll interval: a pending first observation
			// times out without a wall-clock sleep.
			doc, _, err := runComponent(t, client, "--chart", "kagent", "--version", "1.2.4", "--timeout", time.Nanosecond.String())
			if agentcli.Exit(err) != tc.code || (tc.reason != "" && (err == nil || !strings.Contains(err.Error(), tc.reason))) {
				t.Fatalf("want exit %d with %q, got %v", tc.code, tc.reason, err)
			}
			if doc.Version != "1.2.4" || len(doc.Charts) != 1 || doc.Charts[0] != "kagent" {
				t.Errorf("target lost from document: %+v", doc.Result)
			}
		})
	}
}

func TestRunComponentUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "case 0: chart without version", args: []string{"--chart", "kagent"}},
		{name: "case 1: version without chart", args: []string{"--version", "1.2.4"}},
		{name: "case 2: PR instead of version", args: []string{"--chart", "kagent", "--pr", "1"}},
		{name: "case 3: PR with version", args: []string{"--chart", "kagent", "--version", "1.2.4", "--pr", "1"}},
		{name: "case 4: chart path", args: []string{"--chart", "giantswarm/kagent/helm/kagent", "--version", "1.2.4"}},
		{name: "case 5: chart tag", args: []string{"--chart", "kagent:1.2.4", "--version", "1.2.4"}},
		{name: "case 6: invalid version", args: []string{"--chart", "kagent", "--version", "latest"}},
		{name: "case 7: release image", args: []string{"--chart", "kagent", "--version", "1.2.4", "--image", "giantswarm/kagent"}},
		{name: "case 8: release log", args: []string{"--chart", "kagent", "--version", "1.2.4", "--failed-log"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := runComponent(t, nil, tc.args...)
			if agentcli.Exit(err) != agentcli.ExitUsage || doc.Verdict != agentcli.VerdictUsage {
				t.Fatalf("want usage document, got %+v; %v", doc.Envelope, err)
			}
		})
	}
}

// Ensure a chart's inline range is accepted without an OCIRepository too.
func TestRunComponentInlineChart(t *testing.T) {
	objects := componentObjects("1.2.4", "True", "Succeeded", 2)
	hr := objects[1].(*unstructured.Unstructured)
	unstructured.RemoveNestedField(hr.Object, "spec", "chartRef")
	_ = unstructured.SetNestedMap(hr.Object, map[string]any{"spec": map[string]any{"chart": "kagent", "version": ">=1.2.0 <1.3.0"}}, "spec", "chart")
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), objects[1:]...)
	doc, _, err := runComponent(t, client, "--chart", "kagent", "--version", "1.2.4")
	if err != nil || len(doc.Deployments) != 1 || doc.Deployments[0].Follows != "semver >=1.2.0 <1.3.0" {
		t.Fatalf("inline chart range: %+v; %v", doc.Deployments, err)
	}
}

func TestRunComponentMultipleCharts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		code    int
	}{
		{name: "case 0: second chart still behind", version: "1.2.3", code: agentcli.ExitTimeout},
		{name: "case 1: both charts deployed", version: "1.2.4", code: agentcli.ExitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := componentObjects("1.2.4", "True", "Succeeded", 2)
			second := componentObjects(tc.version, "True", "Succeeded", 2)
			source, hr := second[0].(*unstructured.Unstructured), second[1].(*unstructured.Unstructured)
			source.SetName("kagent-crds")
			_ = unstructured.SetNestedField(source.Object, "oci://registry.invalid/charts/kagent-crds", "spec", "url")
			hr.SetName("kagent-crds")
			_ = unstructured.SetNestedField(hr.Object, "kagent-crds", "spec", "chartRef", "name")
			objects = append(objects, source, hr)
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), objects...)
			doc, _, err := runComponent(t, client, "--chart", "kagent", "--chart", "kagent-crds", "--version", "1.2.4", "--timeout", "1ns")
			if agentcli.Exit(err) != tc.code || len(doc.Charts) != 2 || len(doc.Deployments) != 2 {
				t.Fatalf("must wait for both charts: %+v; %v", doc.Result, err)
			}
		})
	}
}

func TestRunComponentReconcile(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), componentObjects("1.2.3", "True", "Succeeded", 2)...)
	_, _, err := runComponent(t, client, "--chart", "kagent", "--version", "1.2.4", "--reconcile", "--timeout", "1ns")
	if agentcli.Exit(err) != agentcli.ExitTimeout {
		t.Fatalf("want pending rollout, got %v", err)
	}
	patches := map[string]int{}
	for _, action := range client.Actions() {
		if action.GetVerb() == "patch" {
			patches[action.GetResource().Resource]++
			patch := action.(k8stesting.PatchAction)
			if patch.GetName() != "kagent" || patch.GetNamespace() != "flux-giantswarm" || !strings.Contains(string(patch.GetPatch()), "reconcile.fluxcd.io/requestedAt") {
				t.Errorf("unexpected reconcile request: %+v", action)
			}
		}
	}
	if len(patches) != 2 || patches["ocirepositories"] != 1 || patches["helmreleases"] != 1 {
		t.Errorf("want one request to the source and HelmRelease: %v", patches)
	}
}
