package wait

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/giantswarm/devctl/v8/internal/laptop"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait/releasewaittest"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

func TestRunValidHelmReleaseReachesRolloutDocument(t *testing.T) {
	t.Setenv(agentcli.EnvTimeScale, "1")
	for _, name := range []string{"devctl", "flux-giantswarm/devctl"} {
		t.Run(name, func(t *testing.T) {
			sources, err := releasewaittest.GeneratedRelease("giantswarm", "devctl", &releasewaittest.Registry{}, "success")
			if err != nil {
				t.Fatal(err)
			}
			hr := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmRelease",
				"metadata": map[string]any{"name": "devctl", "namespace": "flux-giantswarm", "generation": int64(1)},
				"spec":     map[string]any{"chart": map[string]any{"spec": map[string]any{"chart": "devctl", "version": "1.x"}}},
				"status": map[string]any{
					"observedGeneration": int64(1),
					"conditions":         []any{map[string]any{"type": "Ready", "status": "True", "reason": "UpgradeSucceeded"}},
					"history":            []any{map[string]any{"chartName": "devctl", "chartVersion": "1.2.3", "status": "deployed", "name": "devctl", "namespace": "giantswarm"}},
				},
			}}
			client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), rolloutwait.ListKinds(), hr)
			var stdout, stderr bytes.Buffer
			r := &runner{
				flag: &flag{Version: "v1.2.3", HelmReleases: []string{name}},
				gate: func(bool) error { return nil }, stdout: &stdout, stderr: &stderr,
				openCluster: func(context.Context, laptop.ClusterConfig) (dynamic.Interface, error) {
					return client, nil
				},
				openRelease: func(context.Context, string, agentcli.Endpoints, http.RoundTripper, func(string)) (*releasewait.Sources, error) {
					return sources, nil
				},
			}
			err = r.run(context.Background(), []string{"myinstallation", "giantswarm/devctl"})
			if err != nil {
				t.Fatalf("want a completed rollout, got %v", err)
			}
			var doc rolloutwait.Document
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("stdout is not one JSON document: %v", err)
			}
			if doc.Command != rolloutwait.Command || doc.ExitCode != agentcli.ExitOK || doc.Verdict != rolloutwait.VerdictRolledOut || doc.Version != "1.2.3" || doc.Release.Tag != "v1.2.3" || doc.Release.Verdict != agentcli.VerdictAvailable {
				t.Errorf("document: %+v", doc)
			}
			if len(doc.Deployments) != 1 || doc.Deployments[0].State != rolloutwait.StateRolledOut {
				t.Errorf("deployments: %+v", doc.Deployments)
			}
			if stderr.Len() != 0 {
				t.Errorf("an immediately completed wait should be silent: %q", stderr.String())
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "list" {
					t.Errorf("a wait without --reconcile must only list: %v", action)
				}
			}
		})
	}
}
