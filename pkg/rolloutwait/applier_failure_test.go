package rolloutwait

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestRevisionAppliedRevisionFailureIsExit1(t *testing.T) {
	for kind, applier := range map[string]*unstructured.Unstructured{
		KindKustomization: extrasKustomization(),
		KindKonfiguration: konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")),
	} {
		t.Run(kind, func(t *testing.T) {
			reason := "HealthCheckFailed"
			if kind == KindKonfiguration {
				reason = "ReconciliationFailed"
			}
			_ = unstructured.SetNestedField(applier.Object, mergeSHA, "status", "lastAttemptedRevision")
			_ = unstructured.SetNestedSlice(applier.Object, ready("False", reason, "the applied revision is not healthy"), "status", "conditions")
			r := wait(t, newClient(gitRepository(mergeSHA), applier), false, revision("README.md"))
			wantExit(t, r, agentcli.ExitRed, reason+" "+short(mergeSHA))
			if d := r.result.Deployments[len(r.result.Deployments)-1]; d.Kind != kind || d.State != StateFailed {
				t.Errorf("deployment: %+v", d)
			}
		})
	}
}
