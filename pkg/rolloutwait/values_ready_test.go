package rolloutwait

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// readyWithValues is a HelmRelease Ready at running whose last attempt used
// attemptedValues.
func readyWithValues(running, attemptedValues string) *unstructured.Unstructured {
	hr := helmRelease(running, map[string]any{
		"lastAttemptedRevision":     running,
		"lastAttemptedConfigDigest": valuesDigest(attemptedValues),
	})
	_ = unstructured.SetNestedSlice(hr.Object, []any{map[string]any{"kind": "ConfigMap", "name": "values"}}, "spec", "valuesFrom")
	return hr
}

func namedOption(named bool) func(*Config) {
	return func(c *Config) {
		if named {
			c.Charts = []string{"meta-chart"}
			c.HelmReleases = []string{"flux-giantswarm/" + chart}
		}
	}
}

// onReconcile has helm-controller handle the reconcile request at once:
// lastHandledReconcileAt takes its requestedAt, then attempt changes the
// HelmRelease.
func onReconcile(t *testing.T, client interface {
	PrependReactor(string, string, k8stesting.ReactionFunc)
	Tracker() k8stesting.ObjectTracker
}, attempt func(*unstructured.Unstructured)) *int {
	t.Helper()
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
		attempt(hr)
		return true, hr, client.Tracker().Update(helmReleases, hr, patch.GetNamespace())
	})
	return &patches
}

func TestStaleValuesReadyWaits(t *testing.T) {
	for _, named := range []bool{false, true} {
		for _, reconcile := range []bool{false, true} {
			t.Run(fmt.Sprintf("named=%t/reconcile=%t", named, reconcile), func(t *testing.T) {
				client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), readyWithValues("0.48.1", "replicas: 1\n"), valuesConfigMap("replicas: 2\n"), deployment(2))
				r := wait(t, client, reconcile, namedOption(named))
				wantExit(t, r, agentcli.ExitTimeout, "values have changed since the last attempt")
				patches := 0
				for _, a := range client.Actions() {
					if p, ok := a.(k8stesting.PatchAction); ok {
						patches++
						if p.GetResource() != helmReleases || p.GetName() != chart || !strings.Contains(string(p.GetPatch()), "reconcile.fluxcd.io/requestedAt") {
							t.Errorf("unexpected patch: %+v", p)
						}
					}
				}
				wantPatches := 0
				if reconcile {
					wantPatches = 1
				}
				if patches != wantPatches {
					t.Errorf("want %d HelmRelease patches, got %d", wantPatches, patches)
				}
			})
		}
	}
}

func TestCurrentValuesReadyRollsOut(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprintf("named=%t", named), func(t *testing.T) {
			client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), readyWithValues("0.48.1", "replicas: 2\n"), valuesConfigMap("replicas: 2\n"), deployment(2))
			r := wait(t, client, true, namedOption(named))
			wantExit(t, r, agentcli.ExitOK, "")
			for _, a := range client.Actions() {
				if a.GetVerb() == "patch" {
					t.Errorf("unexpected reconcile request: %+v", a)
				}
			}
		})
	}
}

// The values change while the HelmRelease is Ready; the attempt
// --reconcile requests fails on them.
func TestStaleValuesReadyReconciledAttemptFails(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprintf("named=%t", named), func(t *testing.T) {
			client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), readyWithValues("0.48.1", "replicas: 1\n"), valuesConfigMap("replicas: 2\n"), deployment(2))
			patches := onReconcile(t, client, func(hr *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(hr.Object, valuesDigest("replicas: 2\n"), "status", "lastAttemptedConfigDigest")
				_ = unstructured.SetNestedSlice(hr.Object, ready("False", "UpgradeFailed", "new values failed"), "status", "conditions")
			})
			r := wait(t, client, true, namedOption(named))
			wantExit(t, r, agentcli.ExitRed, "new values failed")
			if *patches != 1 {
				t.Errorf("want one reconcile request, got %d", *patches)
			}
		})
	}
}

// helm-controller handled the request and stays Ready, but the digest still
// differs: its verdict stands over a digest that cannot match Flux's.
func TestStaleValuesReadyHandledReconcileTrustsHelmController(t *testing.T) {
	client := newClient(ociRepository(map[string]any{"semver": ">=0.17.0"}), readyWithValues("0.48.1", "replicas: 1\n"), valuesConfigMap("replicas: 2\n"), deployment(2))
	patches := onReconcile(t, client, func(*unstructured.Unstructured) {})
	r := wait(t, client, true)
	wantExit(t, r, agentcli.ExitOK, "")
	if *patches != 1 {
		t.Errorf("want one reconcile request, got %d", *patches)
	}
	if len(r.warnings) == 0 || !strings.Contains(strings.Join(r.warnings, "\n"), "trusting helm-controller") {
		t.Errorf("want a warning that the digest still differs, got %q", r.warnings)
	}
}
