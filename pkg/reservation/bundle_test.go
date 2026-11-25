package reservation_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// A bundle member reservation patches the bundle's HelmRelease and appends one
// post-renderer that patches the member OCIRepository, which exists only after
// helm-controller renders the bundle.

const (
	fixtureBundle      = "observability-bundle"
	fixtureMemberChart = "alloy"
	memberSelector     = "bundle.giantswarm.io/member-chart=alloy"
)

// existingPostRenderer is a post-renderer a person wrote on the bundle.
const existingPostRenderer = `  postRenderers:
    - kustomize:
        patches:
          - target:
              kind: Deployment
            patch: |-
              - op: add
                path: /metadata/labels/team
                value: honeybadger
`

// bundleFixture is a cluster whose collections or extras hold the bundle's
// source and HelmRelease. postRenderers is the spec.postRenderers text the
// HelmRelease starts with, if any.
func bundleFixture(t *testing.T, tree, postRenderers string) string {
	t.Helper()

	return newGitOpsFixture(t, bundleOptions(tree, postRenderers))
}

func bundleOptions(tree, postRenderers string) fixtureOptions {
	namespace := "giantswarm"
	if tree == reservation.GitOpsSourceExtras {
		namespace = "flux-giantswarm"
	}
	manifests := extrasSource(fixtureBundle, namespace, fixtureBundle) + fmt.Sprintf(`---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: %s
  namespace: %s
spec:
  chartRef:
    kind: OCIRepository
    name: %s
    namespace: %s
  interval: 5m
%s`, fixtureBundle, namespace, fixtureBundle, namespace, postRenderers)

	if tree == reservation.GitOpsSourceExtras {
		return fixtureOptions{extraFiles: map[string]string{
			pathExtras: "resources:\n  - ./extras-app/\n  - ./bundle/\n",
			"management-clusters/" + fixtureCluster + "/extras/bundle/kustomization.yaml": "resources:\n  - bundle.yaml\n",
			"management-clusters/" + fixtureCluster + "/extras/bundle/bundle.yaml":        manifests,
		}}
	}

	return fixtureOptions{extraFiles: map[string]string{
		"bases/collections/demo/kustomization.yaml": "resources:\n  - hello-world.yaml\n  - bundle.yaml\n",
		"bases/collections/demo/bundle.yaml":        manifests,
	}}
}

func bundleRequest(dir string) reservation.Request {
	req := testRequest(dir)
	req.App = fixtureMemberChart
	req.Bundle = fixtureBundle
	req.PullRequest = "giantswarm/alloy#5"

	return req
}

// renderedPostRenderers is spec.postRenderers of the bundle HelmRelease in the
// render of tree.
func renderedPostRenderers(t *testing.T, dir, tree string) []any {
	t.Helper()

	hr := mustObject(t, renderTree(t, dir, fixtureCluster, tree), "HelmRelease/"+fixtureBundle)
	prs, _ := hr["spec"].(map[string]any)["postRenderers"].([]any)

	return prs
}

// memberAfter applies one post-renderer, as helm-controller does, to a member
// source that carries label and a stable tag.
func memberAfter(t *testing.T, postRenderer any, label string) map[string]any {
	t.Helper()

	var entry struct {
		Kustomize struct {
			Patches []map[string]any `yaml:"patches"`
		} `yaml:"kustomize"`
	}
	raw, err := yaml.Marshal(postRenderer)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	patches, err := yaml.Marshal(entry.Kustomize.Patches)
	if err != nil {
		t.Fatal(err)
	}

	fs := filesys.MakeFsInMemory()
	member := fmt.Sprintf(`apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: %[1]s-member
  namespace: giantswarm
  labels:
    bundle.giantswarm.io/member-chart: %[1]s
spec:
  interval: 10m
  ref:
    tag: 1.2.3
  url: oci://gsoci.azurecr.io/charts/giantswarm/%[1]s
`, label)
	if err := fs.WriteFile("/r/member.yaml", []byte(member)); err != nil {
		t.Fatal(err)
	}
	k := "resources:\n  - member.yaml\npatches:\n" + indent(string(patches), "  ")
	if err := fs.WriteFile("/r/kustomization.yaml", []byte(k)); err != nil {
		t.Fatal(err)
	}
	res, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(fs, "/r")
	if err != nil {
		t.Fatal(err)
	}
	out, err := res.AsYaml()
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := yaml.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}

	return obj
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+prefix) + "\n"
}

func TestReserveBundleMemberAppendsOnePostRenderer(t *testing.T) {
	for _, tc := range []struct {
		tree, kustomization string
	}{
		{reservation.GitOpsSourceCollections, pathCollections},
		{reservation.GitOpsSourceExtras, pathExtras},
	} {
		t.Run(tc.tree, func(t *testing.T) {
			dir := bundleFixture(t, tc.tree, "")
			req := bundleRequest(dir)

			res, err := reservation.Reserve(req)
			if err != nil {
				t.Fatal(err)
			}

			if res.GitOpsSource != tc.tree {
				t.Errorf("gitops source: got %q, want %q", res.GitOpsSource, tc.tree)
			}
			head, changed := headFiles(t, dir)
			if res.Commit != head {
				t.Errorf("result commit %q is not HEAD %q", res.Commit, head)
			}
			want := []string{tc.kustomization, pathReservations}
			sort.Strings(want)
			if diff := cmp.Diff(want, changed); diff != "" {
				t.Errorf("files in the commit (-want +got):\n%s", diff)
			}
			entry := readFixtureFile(t, dir, pathReservations)
			if !strings.Contains(entry, "bundle: "+fixtureBundle) || !strings.Contains(entry, "gitops-source: "+tc.tree) {
				t.Errorf("the ConfigMap entry lacks the bundle key next to gitops-source:\n%s", entry)
			}

			prs := renderedPostRenderers(t, dir, tc.tree)
			if len(prs) != 1 {
				t.Fatalf("want one post-renderer, got %d: %v", len(prs), prs)
			}
			patches := prs[0].(map[string]any)["kustomize"].(map[string]any)["patches"].([]any)
			for _, p := range patches {
				target := p.(map[string]any)["target"]
				want := map[string]any{"kind": "OCIRepository", "labelSelector": memberSelector}
				if diff := cmp.Diff(want, target); diff != "" {
					t.Errorf("patch target (-want +got):\n%s", diff)
				}
			}

			got := memberAfter(t, prs[0], fixtureMemberChart)
			spec := got["spec"].(map[string]any)
			if diff := cmp.Diff(map[string]any{"semver": ">=0.0.0-0", "semverFilter": res.SemverFilter}, spec["ref"]); diff != "" {
				t.Errorf("member ref, replaced whole (-want +got):\n%s", diff)
			}
			if spec["interval"] != "1m" {
				t.Errorf("member interval: got %v, want 1m", spec["interval"])
			}
			wantAnnotations := map[string]any{
				"reservation.giantswarm.io/user":   testUser,
				"reservation.giantswarm.io/branch": testBranch,
				"reservation.giantswarm.io/pr":     req.PullRequest,
				"reservation.giantswarm.io/scope":  "app",
				"reservation.giantswarm.io/from":   "2026-09-15T10:00:00Z",
				"reservation.giantswarm.io/until":  "2026-09-15T20:00:00Z",
			}
			if diff := cmp.Diff(wantAnnotations, got["metadata"].(map[string]any)["annotations"]); diff != "" {
				t.Errorf("member annotations (-want +got):\n%s", diff)
			}

			other := memberAfter(t, prs[0], "other-chart")
			if other["spec"].(map[string]any)["ref"].(map[string]any)["tag"] != "1.2.3" {
				t.Errorf("a member of another chart moved: %v", other["spec"])
			}
		})
	}
}

// TestReserveBundleMemberKeepsExistingPostRenderers: the reservation entry comes
// after any entry a person wrote, and a release removes only its own.
func TestReserveBundleMemberKeepsExistingPostRenderers(t *testing.T) {
	dir := bundleFixture(t, reservation.GitOpsSourceCollections, existingPostRenderer)
	kustomizationBefore := readFixtureFile(t, dir, pathCollections)
	configMapBefore := readFixtureFile(t, dir, pathReservations)
	before := renderedPostRenderers(t, dir, "collections")

	if _, err := reservation.Reserve(bundleRequest(dir)); err != nil {
		t.Fatal(err)
	}
	after := renderedPostRenderers(t, dir, "collections")
	if len(after) != 2 {
		t.Fatalf("want the existing entry and one reservation entry, got %d: %v", len(after), after)
	}
	if diff := cmp.Diff(before[0], after[0]); diff != "" {
		t.Errorf("the existing post-renderer changed (-want +got):\n%s", diff)
	}

	rel := testReleaseRequest(dir)
	rel.App = fixtureMemberChart
	if _, err := reservation.Release(rel); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(before, renderedPostRenderers(t, dir, "collections")); diff != "" {
		t.Errorf("post-renderers after release (-want +got):\n%s", diff)
	}
	if got := readFixtureFile(t, dir, pathCollections); got != kustomizationBefore {
		t.Errorf("release did not restore the kustomization:\n%s", got)
	}
	if got := readFixtureFile(t, dir, pathReservations); got != configMapBefore {
		t.Errorf("release did not restore the ConfigMap:\n%s", got)
	}
}

func TestReserveBundleRefusesAnUnknownBundle(t *testing.T) {
	dir := bundleFixture(t, reservation.GitOpsSourceCollections, "")
	head, _ := headFiles(t, dir)
	req := bundleRequest(dir)
	req.Bundle = "no-such-bundle"

	_, err := reservation.Reserve(req)
	assertRefused(t, dir, head, err, reservation.IsAppNotFound, "no-such-bundle")
}

// TestExtendAndReapKeepTheBundleOfAMemberReservation: extend rewrites the entry
// and keeps its bundle key; the reaper releases an expired one like any other.
func TestExtendAndReapKeepTheBundleOfAMemberReservation(t *testing.T) {
	dir, _ := newReapFixture(t, bundleOptions(reservation.GitOpsSourceCollections, existingPostRenderer))
	before := renderedPostRenderers(t, dir, "collections")
	req := bundleRequest(dir)
	req.Duration = 4 * time.Hour
	reserveAndPush(t, dir, req)

	if _, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: req.PullRequest,
		User:        testUser,
		Now:         time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if got := reservationEntries(t, dir, fixtureCluster)[fixtureMemberChart]["bundle"]; got != fixtureBundle {
		t.Errorf("entry bundle after extend: got %q, want %q", got, fixtureBundle)
	}

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil || len(reaped) != 1 {
		t.Fatalf("Reap: %v, reaped %+v", err, reaped)
	}
	if diff := cmp.Diff(before, renderedPostRenderers(t, dir, "collections")); diff != "" {
		t.Errorf("post-renderers after the reap (-want +got):\n%s", diff)
	}
}

// reserveSecondMember reserves another chart of the same bundle.
func reserveSecondMember(t *testing.T, dir string) {
	t.Helper()

	req := bundleRequest(dir)
	req.App = "loki"
	req.PullRequest = "giantswarm/loki#6"
	if _, err := reservation.Reserve(req); err != nil {
		t.Fatalf("reserving the second member: %v", err)
	}
}

func releaseMember(t *testing.T, dir, chart string) {
	t.Helper()

	rel := testReleaseRequest(dir)
	rel.App = chart
	if _, err := reservation.Release(rel); err != nil {
		t.Fatalf("releasing %s: %v", chart, err)
	}
}

// TestReleaseOfOneBundleMemberKeepsTheOther: two members of one bundle, on a
// HelmRelease with no post-renderers of its own, in either release order. The
// files end up as if only the other one had been reserved.
func TestReleaseOfOneBundleMemberKeepsTheOther(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"first reserved goes first", fixtureMemberChart, "loki"},
		{"second reserved goes first", "loki", fixtureMemberChart},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := bundleFixture(t, reservation.GitOpsSourceCollections, "")
			original := readFixtureFile(t, dir, pathCollections)
			originalMap := readFixtureFile(t, dir, pathReservations)

			// What reserving only tc.second looks like.
			alone := bundleFixture(t, reservation.GitOpsSourceCollections, "")
			req := bundleRequest(alone)
			req.App, req.PullRequest = tc.second, "giantswarm/"+tc.second+"#"+map[string]string{"alloy": "5", "loki": "6"}[tc.second]
			if _, err := reservation.Reserve(req); err != nil {
				t.Fatal(err)
			}

			if _, err := reservation.Reserve(bundleRequest(dir)); err != nil {
				t.Fatal(err)
			}
			reserveSecondMember(t, dir)

			releaseMember(t, dir, tc.first)
			prs := renderedPostRenderers(t, dir, "collections")
			if len(prs) != 1 {
				t.Fatalf("want only the entry of %s, got %d", tc.second, len(prs))
			}
			if got, want := readFixtureFile(t, dir, pathCollections), readFixtureFile(t, alone, pathCollections); got != want {
				t.Errorf("kustomization is not the one of a lone %s reservation:\n%s\nwant\n%s", tc.second, got, want)
			}

			releaseMember(t, dir, tc.second)
			if got := readFixtureFile(t, dir, pathCollections); got != original {
				t.Errorf("kustomization not restored:\n%s", got)
			}
			if got := readFixtureFile(t, dir, pathReservations); got != originalMap {
				t.Errorf("ConfigMap not restored:\n%s", got)
			}
		})
	}
}

// TestReapOfOneBundleMemberKeepsTheOther: the reaper removes an expired member
// whose block created the post-renderer list, and the other member still renders.
func TestReapOfOneBundleMemberKeepsTheOther(t *testing.T) {
	dir, _ := newReapFixture(t, bundleOptions(reservation.GitOpsSourceCollections, ""))
	short := bundleRequest(dir)
	short.Duration = time.Hour
	reserveAndPush(t, dir, short)
	long := bundleRequest(dir)
	long.App, long.PullRequest, long.Duration = "loki", "giantswarm/loki#6", 10*time.Hour
	reserveAndPush(t, dir, long)

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || len(reaped) != 1 || reaped[0].App != fixtureMemberChart {
		t.Fatalf("Reap: %v, reaped %+v", err, reaped)
	}
	prs := renderedPostRenderers(t, dir, "collections")
	if len(prs) != 1 {
		t.Fatalf("want the entry of loki only, got %d", len(prs))
	}
	if got := memberAfter(t, prs[0], "loki")["spec"].(map[string]any)["interval"]; got != "1m" {
		t.Errorf("the remaining entry does not move loki: interval %v", got)
	}
}
