package rolloutwait

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestChildHelmReleaseRetriesUntilSettled(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		name := "case 0: settled failure"
		if recovered {
			name = "case 1: recovered"
		}
		t.Run(name, func(t *testing.T) {
			client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), deployment(2))
			polls := 0
			client.PrependReactor("list", "helmreleases", func(k8stesting.Action) (bool, runtime.Object, error) {
				polls++
				conditions := ready("False", "UpgradeFailed", "timed out")
				if polls == 1 {
					conditions = append(conditions, map[string]any{"type": "Reconciling", "status": "True", "reason": "ProgressingWithRetry", "message": "retrying"})
				} else if recovered {
					conditions = ready("True", "UpgradeSucceeded", "")
				}
				return true, &unstructured.UnstructuredList{
					Object: map[string]any{"apiVersion": "helm.toolkit.fluxcd.io/v2", "kind": "HelmReleaseList"},
					Items:  []unstructured.Unstructured{*helmRelease("0.48.1", nil), *childHelmRelease(2, conditions)},
				}, nil
			})
			r := wait(t, client, false)
			code := agentcli.ExitRed
			if recovered {
				code = agentcli.ExitOK
			}
			wantExit(t, r, code, "")
			if polls != 2 {
				t.Errorf("want the retry and its settled outcome read, got %d polls", polls)
			}
		})
	}
}
