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

// valuesApp is an App that takes its config from the rendered ConfigMap,
// pinned to 1.2.0, whose release last deployed at deployed.
func valuesApp(deployed time.Time, version, releaseStatus string) *unstructured.Unstructured {
	return obj("application.giantswarm.io/v1alpha1", "App", "giantswarm", "agent-platform", map[string]any{
		"spec": map[string]any{
			"name": "agent-platform", "version": "1.2.0", "catalog": "control-plane-catalog", "namespace": "agent-platform",
			"kubeConfig": map[string]any{"inCluster": true},
			"config":     map[string]any{"configMap": map[string]any{"name": "agent-platform-konfiguration", "namespace": "flux-giantswarm"}},
		},
		"status": map[string]any{"version": version, "release": map[string]any{"status": releaseStatus, "lastDeployed": deployed.Format(time.RFC3339)}},
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

// An App that takes the rendered values is followed like a HelmRelease:
// named with its chart and deployed version, rolled out once it deployed
// its pinned version after the values were rendered.
func TestRevisionAppRolledOut(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), valuesApp(renderedAt.Add(time.Minute), "1.2.0", "deployed")), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitOK, "")
	d := r.result.Deployments[len(r.result.Deployments)-1]
	if d.Kind != KindApp || d.Name != "agent-platform" || d.Chart != "agent-platform" || d.RunningVersion != "1.2.0" || d.State != StateRolledOut {
		t.Errorf("App: %+v", d)
	}
}

// An App not deployed since the values were rendered keeps the wait open.
func TestRevisionAppNotRedeployedIsATimeout(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), valuesApp(renderedAt.Add(-time.Hour), "1.2.0", "deployed")), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitTimeout, "App giantswarm/agent-platform: last deployed")
}

// An App whose release failed with the new values ends the wait.
func TestRevisionAppFailedIsExit1(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), valuesApp(renderedAt.Add(time.Minute), "1.2.0", "failed")), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitRed, "App giantswarm/agent-platform failed")
}

// The values of a changed app that no HelmRelease or App takes: the
// change is applied, whether it runs cannot be told, and it is not
// reported rolled out.
func TestRevisionValuesNothingTakesAreUnknown(t *testing.T) {
	r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA)), false, revision(agentPlatformValues))
	wantExit(t, r, agentcli.ExitNotApplicable, "ConfigMap flux-giantswarm/agent-platform-konfiguration: no HelmRelease or App on myinstallation takes these values")
	if _, verdict := agentcli.Outcome(r.err); verdict != VerdictRolloutUnknown {
		t.Errorf("verdict %s", verdict)
	}
	if d := r.result.Deployments[len(r.result.Deployments)-1]; d.Kind != KindConfigMap || d.State != StateUnknown {
		t.Errorf("deployment: %+v", d)
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

// mergedAt is when the pull request merged: days before konfigure-operator
// last re-labelled the rendered values (renderedAt) with a later revision.
var mergedAt = renderedAt.Add(-72 * time.Hour)

func mergedAtOption(c *Config) { c.MergedAt = mergedAt }

// deploy is one entry of a HelmRelease's status.history: when it deployed
// and the digest of its values.
type deploy struct {
	at     time.Time
	digest string
}

// withHistory sets the status.history of a HelmRelease, newest first.
func withHistory(hr *unstructured.Unstructured, deploys ...deploy) *unstructured.Unstructured {
	var history []any
	for i, d := range deploys {
		status := "superseded"
		if i == 0 {
			status = "deployed"
		}
		history = append(history, map[string]any{
			"chartName": hr.GetName(), "chartVersion": "4.109.0+390ef739c266", "status": status, "name": hr.GetName(), "namespace": "agent-platform",
			"lastDeployed": d.at.Format(time.RFC3339), "configDigest": d.digest,
		})
	}
	_ = unstructured.SetNestedSlice(hr.Object, history, "status", "history")
	return hr
}

// A HelmRelease that took new values after the merge runs the change,
// although konfigure-operator re-rendered its ConfigMap since with a later
// revision that left the values as they were.
func TestRevisionHelmReleaseNewValuesAfterMergeRolledOut(t *testing.T) {
	upgraded := withHistory(valuesHelmRelease("agent-platform", time.Time{}, ready("True", "UpgradeSucceeded", "")),
		deploy{mergedAt.Add(2 * time.Minute), "sha256:new"}, deploy{mergedAt.Add(-24 * time.Hour), "sha256:old"})
	r := wait(t, newClient(gitRepository(newerSHA), konfiguration(newerSHA, newerSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(newerSHA), upgraded), false, revision(agentPlatformValues), mergedAtOption)
	wantExit(t, r, agentcli.ExitOK, "")
}

// A deploy after the merge with the values of the one before (a chart
// upgrade before konfigure-operator rendered the change) does not, nor
// does a values change before the merge.
func TestRevisionHelmReleaseNoNewValuesAfterMergeIsATimeout(t *testing.T) {
	for name, deploys := range map[string][]deploy{
		"same values after the merge": {{mergedAt.Add(time.Minute), "sha256:old"}, {mergedAt.Add(-24 * time.Hour), "sha256:old"}},
		"new values before the merge": {{mergedAt.Add(-time.Minute), "sha256:new"}, {mergedAt.Add(-24 * time.Hour), "sha256:old"}},
	} {
		t.Run(name, func(t *testing.T) {
			stale := withHistory(valuesHelmRelease("agent-platform", time.Time{}, ready("True", "UpgradeSucceeded", "")), deploys...)
			r := wait(t, newClient(gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), stale), false, revision(agentPlatformValues), mergedAtOption)
			wantExit(t, r, agentcli.ExitTimeout, "HelmRelease flux-giantswarm/agent-platform: last deployed")
		})
	}
}

// appChart is the Chart app-operator creates for the App and the values it
// merged for it, last written at written.
func appChart(written time.Time) []runtime.Object {
	chart := obj("application.giantswarm.io/v1alpha1", "Chart", "giantswarm", "agent-platform", map[string]any{
		"spec": map[string]any{"config": map[string]any{"configMap": map[string]any{"name": "agent-platform-chart-values", "namespace": "giantswarm"}}},
	})
	values := obj("v1", "ConfigMap", "giantswarm", "agent-platform-chart-values", nil)
	values.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "app-operator", Operation: metav1.ManagedFieldsOperationUpdate, Time: &metav1.Time{Time: written}}})
	return []runtime.Object{chart, values}
}

// An App whose merged values app-operator changed after the merge, and that
// deployed them, runs the change although its ConfigMap was re-rendered
// since; values merged before the merge keep the wait open.
func TestRevisionAppNewValuesAfterMerge(t *testing.T) {
	for name, tc := range map[string]struct {
		written time.Time
		code    int
	}{
		"after the merge":  {mergedAt.Add(time.Minute), agentcli.ExitOK},
		"before the merge": {mergedAt.Add(-time.Minute), agentcli.ExitTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			objects := append([]runtime.Object{gitRepository(mergeSHA), konfiguration(mergeSHA, mergeSHA, ready("True", "ReconciliationSucceeded", "")), renderedConfigMap(mergeSHA), valuesApp(tc.written, "1.2.0", "deployed")}, appChart(tc.written)...)
			r := wait(t, newClient(objects...), false, revision(agentPlatformValues), mergedAtOption)
			if agentcli.Exit(r.err) != tc.code {
				t.Errorf("exit %d, want %d: %v", agentcli.Exit(r.err), tc.code, r.err)
			}
		})
	}
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
		"installations/myinstallation/apps/agent-platform/configmap-values.yaml.patch",
		"default/apps/muster/configmap-values.yaml.template",
		"installations/other/apps/backstage/configmap-values.yaml.patch",
		"installations/myinstallation/config.yaml.patch",
		"default/config.yaml",
		".github/workflows/gitleaks.yaml",
		"README.md",
	}, "myinstallation")
	if len(apps) != 2 || !apps["agent-platform"] || !apps["muster"] {
		t.Errorf("apps: %v", apps)
	}
	if !slices.Equal(global, []string{"installations/myinstallation/config.yaml.patch", "default/config.yaml"}) {
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
