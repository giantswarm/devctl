package rolloutwait

import (
	"encoding/json"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func failedValuesConditions(at time.Time) []any {
	return []any{map[string]any{
		"type": "Ready", "status": "False", "reason": "UpgradeFailed", "message": "bad values",
		"lastTransitionTime": at.Format(time.RFC3339),
	}}
}

func TestRevisionNewValuesFailedAttemptIsExit1(t *testing.T) {
	failed := withHistory(valuesHelmRelease("agent-platform", time.Time{}, failedValuesConditions(renderedAt.Add(time.Minute))),
		deploy{renderedAt.Add(time.Minute), "sha256:new"}, deploy{mergedAt.Add(-time.Hour), "sha256:old"})
	history, _, _ := unstructured.NestedSlice(failed.Object, "status", "history")
	history[0].(map[string]any)["status"] = "failed"
	history[1].(map[string]any)["status"] = "deployed"
	_ = unstructured.SetNestedSlice(failed.Object, history, "status", "history")
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), failed), false, revision(agentPlatformValues), mergedAtOption)
	wantExit(t, r, agentcli.ExitRed, "UpgradeFailed with the values of")
	if d := r.result.Deployments[len(r.result.Deployments)-1]; d.State != StateFailed {
		t.Errorf("deployment: %+v", d)
	}
}

func TestRevisionHandledReconcileFailureIsExit1(t *testing.T) {
	written := mergedAt.Add(2 * time.Minute)
	stale := devctlHelmRelease(mergedAt.Add(-time.Hour), ready("True", "UpgradeSucceeded", ""))
	client := newClient(gitRepository(mergeSHA), extrasKustomization(), devctlValues(written), stale)
	patches := 0
	client.PrependReactor("patch", "helmreleases", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patches++
		patch := action.(k8stesting.PatchAction)
		var body struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(patch.GetPatch(), &body); err != nil {
			return true, nil, err
		}
		o, err := client.Tracker().Get(helmReleases, patch.GetNamespace(), patch.GetName())
		if err != nil {
			return true, nil, err
		}
		hr := o.(*unstructured.Unstructured).DeepCopy()
		_ = unstructured.SetNestedField(hr.Object, body.Metadata.Annotations["reconcile.fluxcd.io/requestedAt"], "status", "lastHandledReconcileAt")
		_ = unstructured.SetNestedSlice(hr.Object, failedValuesConditions(written.Add(time.Minute)), "status", "conditions")
		return true, hr, client.Tracker().Update(helmReleases, hr, patch.GetNamespace())
	})
	r := wait(t, client, true, revision(extrasPath), mergedAtOption)
	wantExit(t, r, agentcli.ExitRed, "UpgradeFailed with the values of")
	if patches != 1 {
		t.Errorf("want one handled reconcile, got %d", patches)
	}
}

func TestRevisionValuesFailureMustSettle(t *testing.T) {
	written := mergedAt.Add(2 * time.Minute)
	for _, retrying := range []bool{false, true} {
		name := "generation unobserved"
		if retrying {
			name = "retrying"
		}
		t.Run(name, func(t *testing.T) {
			conditions := failedValuesConditions(written.Add(time.Minute))
			if retrying {
				conditions = append(conditions, map[string]any{"type": "Reconciling", "status": "True", "reason": "ProgressingWithRetry"})
			}
			hr := devctlHelmRelease(mergedAt.Add(-time.Hour), conditions)
			if !retrying {
				hr.SetGeneration(2)
			}
			r := wait(t, newClient(gitRepository(mergeSHA), extrasKustomization(), devctlValues(written), hr), false, revision(extrasPath), mergedAtOption)
			wantExit(t, r, agentcli.ExitTimeout, "waiting for helm-controller to settle")
		})
	}
}
