package reservation_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const pathExtras = "management-clusters/" + fixtureCluster + "/extras/kustomization.yaml"

func extrasRequest(dir string) reservation.Request {
	req := testRequest(dir)
	req.App = fixtureExtrasApp
	req.PullRequest = "giantswarm/extras-app#7"

	return req
}

// headFiles returns the files the HEAD commit changed, sorted.
func headFiles(t *testing.T, dir string) (string, []string) {
	t.Helper()

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := commit.Stats()
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for _, s := range stats {
		changed = append(changed, s.Name)
	}
	sort.Strings(changed)

	return head.Hash().String(), changed
}

func assertClean(t *testing.T, dir string) {
	t.Helper()

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.IsClean() {
		t.Errorf("working tree is not clean:\n%s", status)
	}
}

// TestReserveExtrasAppendsOneBlock: an extras reservation adds no object and no
// file. It is one marked block at the end of the root patches: list, plus the
// ConfigMap entry, in one commit.
func TestReserveExtrasAppendsOneBlock(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	before := readFixtureFile(t, dir, pathExtras)

	res, err := reservation.Reserve(extrasRequest(dir))
	if err != nil {
		t.Fatal(err)
	}

	want := []string{pathReservations, pathExtras}
	sort.Strings(want)
	if diff := cmp.Diff(want, res.Files); diff != "" {
		t.Errorf("written files (-want +got):\n%s", diff)
	}
	head, changed := headFiles(t, dir)
	if res.Commit != head {
		t.Errorf("result commit %q is not HEAD %q", res.Commit, head)
	}
	if diff := cmp.Diff(want, changed); diff != "" {
		t.Errorf("files in the commit (-want +got):\n%s", diff)
	}
	assertClean(t, dir)

	// Everything people wrote stays where it was; the block only follows it,
	// so it runs after every other patch.
	after := readFixtureFile(t, dir, pathExtras)
	if !strings.HasPrefix(after, before) {
		t.Fatalf("the reservation changed the existing lines of %s:\n%s", pathExtras, after)
	}
	block := strings.TrimSpace(strings.TrimPrefix(after, before))
	lines := strings.Split(block, "\n")
	first, last := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[len(lines)-1])
	for _, marker := range []string{first, last} {
		if !strings.HasPrefix(marker, "#") || !strings.Contains(marker, fixtureExtrasApp) || !strings.Contains(marker, "devctl") {
			t.Errorf("the block is not enclosed in markers that name the chart and devctl: %q", marker)
		}
	}

	if res.SourceName != fixtureExtrasApp {
		t.Errorf("source name: got %q, want the patched source %q", res.SourceName, fixtureExtrasApp)
	}
}

// TestReserveExtrasPatchesTheSourceInPlace: the render shows the app's own
// source following the dev builds, and nothing else of it changes.
func TestReserveExtrasPatchesTheSourceInPlace(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	original := mustObject(t, renderExtras(t, dir, fixtureCluster), "OCIRepository/"+fixtureExtrasApp)
	req := extrasRequest(dir)

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	want := deepCopyYAML(t, original)
	want["metadata"].(map[string]any)["annotations"] = map[string]any{
		"reservation.giantswarm.io/user":   testUser,
		"reservation.giantswarm.io/branch": testBranch,
		"reservation.giantswarm.io/pr":     req.PullRequest,
		"reservation.giantswarm.io/scope":  "app",
		"reservation.giantswarm.io/from":   "2026-09-15T10:00:00Z",
		"reservation.giantswarm.io/until":  "2026-09-15T20:00:00Z",
	}
	want["spec"].(map[string]any)["interval"] = "1m"
	// The whole ref is replaced: a stable semver or tag left next to the
	// filter would select a release, not the branch.
	want["spec"].(map[string]any)["ref"] = map[string]any{
		"semver":       ">=0.0.0-0",
		"semverFilter": res.SemverFilter,
	}

	after := renderExtras(t, dir, fixtureCluster)
	if diff := cmp.Diff(want, mustObject(t, after, "OCIRepository/"+fixtureExtrasApp)); diff != "" {
		t.Errorf("patched source (-want +got):\n%s", diff)
	}
	if _, ok := after["OCIRepository/"+fixtureExtrasApp+"-dev-reservation"]; ok {
		t.Error("an extras reservation added a source object; flux-extras never prunes it")
	}
}

func extrasReleaseRequest(dir string) reservation.ReleaseRequest {
	req := testReleaseRequest(dir)
	req.App = fixtureExtrasApp

	return req
}

// TestReserveExtrasRefusesABlockThatReachesNothing is guard 1: kustomize skips
// a patch whose target matches nothing without an error. A namePrefix in the
// extras renames the source after the patches run, so the block reaches
// nothing, and the reserve must refuse and leave the repo as it was. The
// release still takes its chart from the renamed source, as kustomize does not
// know the chartRef field.
func TestReserveExtrasRefusesABlockThatReachesNothing(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		pathExtras: `namePrefix: pre-
resources:
- ./extras-app/
patches:
- target:
    kind: HelmRelease
    name: extras-app
  patch: |-
    - op: replace
      path: /spec/chartRef/name
      value: pre-extras-app
`,
	}})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(extrasRequest(dir))
	if !reservation.IsRenderAssertion(err) {
		t.Fatalf("expected a render assertion error, got %v", err)
	}
	if after, _ := headFiles(t, dir); after != head {
		t.Errorf("a refused reserve made a commit")
	}
	assertClean(t, dir)
}

// TestReleaseExtrasRestoresTheRepoByteForByte: release removes the block and
// the entry, so the extras come back exactly as Reserve found them, whatever
// shape the patches: list had.
func TestReleaseExtrasRestoresTheRepoByteForByte(t *testing.T) {
	for name, kustomization := range map[string]string{
		"existing patches": "",
		"no patches key": `resources:
- ./extras-app/
`,
		"empty patches before other content": `patches: []
resources:
- ./extras-app/
`,
	} {
		t.Run(name, func(t *testing.T) {
			opts := fixtureOptions{}
			if kustomization != "" {
				opts.extraFiles = map[string]string{pathExtras: kustomization}
			}
			dir := newGitOpsFixture(t, opts)
			before := readFixtureFile(t, dir, pathExtras)
			beforeConfigMap := readFixtureFile(t, dir, pathReservations)

			if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
				t.Fatal(err)
			}
			res, err := reservation.Release(extrasReleaseRequest(dir))
			if err != nil {
				t.Fatal(err)
			}

			commit, files := headFiles(t, dir)
			if res.Commit != commit {
				t.Errorf("result commit %q is not HEAD %q", res.Commit, commit)
			}
			if res.GitOpsSource != "extras" {
				t.Errorf("result gitops source: got %q, want extras", res.GitOpsSource)
			}
			want := []string{pathReservations, pathExtras}
			if diff := cmp.Diff(want, files); diff != "" {
				t.Errorf("release commit files (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(before, readFixtureFile(t, dir, pathExtras)); diff != "" {
				t.Errorf("extras kustomization after release (-before +after):\n%s", diff)
			}
			if diff := cmp.Diff(beforeConfigMap, readFixtureFile(t, dir, pathReservations)); diff != "" {
				t.Errorf("ConfigMap after release (-before +after):\n%s", diff)
			}
			assertClean(t, dir)
		})
	}
}

// TestReserveExtrasTwiceKeepsOneBlock: a second reserve of the same app, after
// the first one expired, replaces the block instead of stacking a second one.
func TestReserveExtrasTwiceKeepsOneBlock(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := reservation.Release(extrasReleaseRequest(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(readFixtureFile(t, dir, pathExtras), "# BEGIN"); n != 1 {
		t.Errorf("got %d blocks, want 1", n)
	}
}

// extrasSourceOnly is an extras OCIRepository with no HelmRelease next to it,
// as when a nested Flux Kustomization takes its chart from it.
func extrasSourceOnly(chart string) string {
	return `apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: ` + chart + `
  namespace: giantswarm
spec:
  interval: 10m
  ref:
    semver: x.x.x
  url: oci://gsoci.azurecr.io/charts/giantswarm/` + chart + `
`
}

// TestReserveFallsBackToCollectionsWhenTheExtrasSourceMovesNothing: an extras
// source of the chart that no HelmRelease uses must not block a reservation of
// the collection app that serves the same chart.
func TestReserveFallsBackToCollectionsWhenTheExtrasSourceMovesNothing(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		pathExtras: `resources:
- ./extras-app/
- ./hello-world-source.yaml
`,
		"management-clusters/" + fixtureCluster + "/extras/hello-world-source.yaml": extrasSourceOnly(fixtureApp),
	}})

	res, err := reservation.Reserve(testRequest(dir))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if res.GitOpsSource != reservation.GitOpsSourceCollections {
		t.Errorf("gitops source: got %q, want %q", res.GitOpsSource, reservation.GitOpsSourceCollections)
	}
}

// TestReserveRefusesAnExtrasSourceThatMovesNothing: a chart that only an extras
// source with no HelmRelease serves keeps the refusal that names that source.
func TestReserveRefusesAnExtrasSourceThatMovesNothing(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		"management-clusters/" + fixtureCluster + "/extras/extras-app/extras-app.yaml": extrasSourceOnly(fixtureExtrasApp),
	}})

	_, err := reservation.Reserve(extrasRequest(dir))
	if !reservation.IsAppNotFound(err) || !strings.Contains(err.Error(), "would change nothing") {
		t.Fatalf("expected the extras refusal that names the unused source, got %v", err)
	}
	assertClean(t, dir)
}
