package rolloutwait

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The merge commit of a configuration change, an older commit and a newer
// one that contains it.
const (
	mergeSHA = "6aeb7ac319a0d15007a8d4342dbbd7f2fadb9c63"
	olderSHA = "1111111111111111111111111111111111111111"
	newerSHA = "2222222222222222222222222222222222222222"
)

var renderedAt = time.Date(2026, 10, 2, 17, 38, 5, 0, time.UTC)

// history answers Reachable from the commits newerSHA contains.
type history struct{ asked []string }

func (h *history) Reachable(_ context.Context, owner, repo, ref, branch string) (bool, error) {
	h.asked = append(h.asked, branch)
	if owner+"/"+repo != "giantswarm/giantswarm-configs" || ref != mergeSHA {
		return false, errors.New("unexpected comparison")
	}
	return branch == newerSHA, nil
}

func gitRepository(sha string) *unstructured.Unstructured {
	return obj("source.toolkit.fluxcd.io/v1", "GitRepository", "flux-giantswarm", "giantswarm-config", map[string]any{
		"spec":   map[string]any{"url": "ssh://git@ssh.github.com:443/giantswarm/giantswarm-configs.git", "ref": map[string]any{"branch": "main"}},
		"status": map[string]any{"artifact": map[string]any{"revision": "main@sha1:" + sha}, "conditions": ready("True", "Succeeded", "")},
	})
}

func konfiguration(applied, attempted string, conditions []any) *unstructured.Unstructured {
	return obj("konfigure.giantswarm.io/v1alpha1", "Konfiguration", "flux-giantswarm", "agent-platform-konfiguration", map[string]any{
		"spec": map[string]any{
			"destination": map[string]any{"namespace": "flux-giantswarm", "naming": map[string]any{"suffix": "konfiguration", "useSeparator": true}},
			"sources":     map[string]any{"flux": map[string]any{"gitRepository": map[string]any{"name": "giantswarm-config", "namespace": "flux-giantswarm"}}},
			"targets":     map[string]any{"iterations": map[string]any{"agent-platform": map[string]any{}}},
		},
		"status": map[string]any{"observedGeneration": int64(1), "lastAppliedRevision": applied, "lastAttemptedRevision": attempted, "conditions": conditions},
	})
}

// renderedConfigMap is what konfigure-operator renders from sha, written at
// renderedAt.
func renderedConfigMap(sha string) *unstructured.Unstructured {
	u := obj("v1", "ConfigMap", "flux-giantswarm", "agent-platform-konfiguration", nil)
	u.SetLabels(map[string]string{labelRevision: sha})
	u.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "manager", Operation: metav1.ManagedFieldsOperationUpdate, Time: &metav1.Time{Time: renderedAt}}})
	return u
}

// valuesHelmRelease is the agent-platform HelmRelease that takes its values
// from the rendered ConfigMap, last deployed at deployed.
func valuesHelmRelease(name string, deployed time.Time, conditions []any) *unstructured.Unstructured {
	return obj("helm.toolkit.fluxcd.io/v2", "HelmRelease", "flux-giantswarm", name, map[string]any{
		"spec": map[string]any{
			"chartRef":   map[string]any{"kind": "OCIRepository", "name": name},
			"valuesFrom": []any{map[string]any{"kind": "ConfigMap", "name": name + "-konfiguration"}, map[string]any{"kind": "Secret", "name": name + "-konfiguration", "optional": true}},
		},
		"status": map[string]any{
			"observedGeneration": int64(1),
			"conditions":         conditions,
			"history": []any{map[string]any{
				"chartName": name, "chartVersion": "4.109.0+390ef739c266", "status": "deployed", "name": name, "namespace": "agent-platform",
				"lastDeployed": deployed.Format(time.RFC3339),
			}},
		},
	})
}

func revision(paths ...string) func(*Config) {
	return func(c *Config) {
		c.Version, c.Charts = "", nil
		c.Revision, c.Repository, c.Paths, c.GitHub = mergeSHA, "giantswarm/giantswarm-configs", paths, &history{}
	}
}

const agentPlatformValues = "installations/myinstallation/apps/agent-platform/configmap-values.yaml.patch"

// A configuration change: the GitRepository fetched the merge commit, the
// Konfiguration applied it, and the HelmRelease of the app it changed
// upgraded after the values were rendered. No version is involved.
func TestRevisionRolledOut(t *testing.T) {
	upgraded := valuesHelmRelease("agent-platform", renderedAt.Add(22*time.Second), ready("True", "UpgradeSucceeded", ""))
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), upgraded), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitOK, "")
	if r.result.Revision != mergeSHA || r.result.Version != "" {
		t.Errorf("result: %+v", r.result)
	}
	var kinds []string
	for _, d := range r.result.Deployments {
		kinds = append(kinds, d.Kind)
		if d.State != StateRolledOut {
			t.Errorf("%s: %s %s", d.id(), d.State, d.Message)
		}
	}
	if !slices.Equal(kinds, []string{KindGitRepository, KindKonfiguration, KindHelmRelease}) {
		t.Errorf("deployments: %v", kinds)
	}
}

// A HelmRelease of an app the change did not touch is not followed: its
// values did not change and it never upgrades.
func TestRevisionUntouchedAppIsNotFollowed(t *testing.T) {
	stale := valuesHelmRelease("agent-platform", renderedAt.Add(-time.Hour), ready("True", "UpgradeSucceeded", ""))
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), stale), false, revision("installations/other/apps/agent-platform/configmap-values.yaml.patch", "README.md"))
	wantExit(t, r, agentcli.ExitOK, "")
	if len(r.result.Deployments) != 2 {
		t.Errorf("deployments: %+v", r.result.Deployments)
	}
}

// A HelmRelease that has not deployed since the values were rendered keeps
// the wait open, and the timeout names it.
func TestRevisionHelmReleaseNotUpgradedIsATimeout(t *testing.T) {
	stale := valuesHelmRelease("agent-platform", renderedAt.Add(-time.Hour), ready("True", "UpgradeSucceeded", ""))
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), stale), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitTimeout, "HelmRelease flux-giantswarm/agent-platform: last deployed")
}

// With --reconcile, a HelmRelease that handled the request made after its
// values were rendered has read them, upgraded or not.
func TestRevisionReconcileHandledCountsAsRolledOut(t *testing.T) {
	stale := valuesHelmRelease("agent-platform", renderedAt.Add(-time.Hour), ready("True", "UpgradeSucceeded", ""))
	client := newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), stale)
	// helm-controller handles the request: lastHandledReconcileAt takes
	// the annotation's value.
	client.PrependReactor("patch", "helmreleases", func(action k8stesting.Action) (bool, runtime.Object, error) {
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
		hr := o.(*unstructured.Unstructured)
		_ = unstructured.SetNestedField(hr.Object, body.Metadata.Annotations["reconcile.fluxcd.io/requestedAt"], "status", "lastHandledReconcileAt")
		return true, hr, client.Tracker().Update(helmReleases, hr, patch.GetNamespace())
	})
	r := wait(t, client, true, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitOK, "")
}

// The GitRepository is at a newer commit that contains the merge commit,
// asked once; the Konfiguration still applies an older one.
func TestRevisionNewerCommitContainsTheMerge(t *testing.T) {
	r := wait(t, newClient(gitRepository(newerSHA), konfiguration(olderSHA, olderSHA, ready("True", "ReconciliationSucceeded", ""))), false, revision("README.md"))
	wantExit(t, r, agentcli.ExitTimeout, "Konfiguration flux-giantswarm/agent-platform-konfiguration: applied 111111111111; waiting for 6aeb7ac319a0")
	if r.result.Deployments[0].State != StateRolledOut {
		t.Errorf("the GitRepository at a newer commit has it: %+v", r.result.Deployments[0])
	}
}

// A Konfiguration that failed to render the revision ends the wait.
func TestRevisionKonfigurationFailedIsExit1(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(olderSHA, mergeSHA, ready("False", "ReconciliationFailed", "rendering agent-platform: bad template"))), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitRed, "ReconciliationFailed 6aeb7ac319a0: rendering agent-platform: bad template")
}

// A Kustomization that reads the GitRepository rolls the revision out once
// it applied it.
func TestRevisionKustomizationApplied(t *testing.T) {
	ks := obj("kustomize.toolkit.fluxcd.io/v1", "Kustomization", "flux-giantswarm", "flux", map[string]any{
		"spec":   map[string]any{"path": "./management-clusters/myinstallation", "sourceRef": map[string]any{"kind": "GitRepository", "name": "giantswarm-config"}},
		"status": map[string]any{"observedGeneration": int64(1), "lastAppliedRevision": "main@sha1:" + mergeSHA, "conditions": ready("True", "ReconciliationSucceeded", "")},
	})
	r := wait(t, newClient(gitRepository(mergeSHA), ks), false, revision("management-clusters/myinstallation/kustomization.yaml"))
	wantExit(t, r, agentcli.ExitOK, "")
}

// Nothing on the installation fetches the repository: no release follows
// the pull request there.
func TestRevisionNotFetchedIsNoRelease(t *testing.T) {
	r := wait(t, newClient(ociRepository(nil)), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitNotApplicable, "no Flux GitRepository on myinstallation fetches giantswarm/giantswarm-configs")
	if _, verdict := agentcli.Outcome(r.err); verdict != agentcli.VerdictNoRelease {
		t.Errorf("verdict %s", verdict)
	}
}

// A GitRepository nothing reads (one included into another) is named.
func TestRevisionUnreadGitRepositoryIsNotApplicable(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA)), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitNotApplicable, "no Kustomization or Konfiguration on myinstallation reads GitRepository flux-giantswarm/giantswarm-config")
}

// Values beyond one app's are warned about, not followed.
func TestRevisionGlobalChangeWarns(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", ""))), false, revision("installations/myinstallation/config.yaml.patch"))
	wantExit(t, r, agentcli.ExitOK, "")
	if len(r.warnings) != 1 || !strings.Contains(r.warnings[0], "installations/myinstallation/config.yaml.patch change values beyond one app's") {
		t.Errorf("warnings: %q", r.warnings)
	}
}

func TestAffectedApps(t *testing.T) {
	apps, global := AffectedApps([]string{
		"installations/glean/apps/agent-platform/configmap-values.yaml.patch",
		"default/apps/muster/configmap-values.yaml.template",
		"installations/gazelle/apps/backstage/configmap-values.yaml.patch",
		"installations/glean/config.yaml.patch",
		"default/config.yaml",
		".github/workflows/gitleaks.yaml",
		"README.md",
	}, "glean")
	if len(apps) != 2 || !apps["agent-platform"] || !apps["muster"] {
		t.Errorf("apps: %v", apps)
	}
	if !slices.Equal(global, []string{"installations/glean/config.yaml.patch", "default/config.yaml"}) {
		t.Errorf("global: %v", global)
	}
}

func TestRevisionSHA(t *testing.T) {
	for in, want := range map[string]string{
		"main@sha1:" + mergeSHA: mergeSHA,
		"sha1:" + mergeSHA:      mergeSHA,
		"main/" + mergeSHA:      mergeSHA,
		mergeSHA:                mergeSHA,
		"":                      "",
	} {
		if got := revisionSHA(in); got != want {
			t.Errorf("revisionSHA(%q) = %q", in, got)
		}
	}
}
