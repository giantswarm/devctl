package reservation_test

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func testReleaseRequest(dir string) reservation.ReleaseRequest {
	return reservation.ReleaseRequest{
		RepoDir: dir,
		Cluster: fixtureCluster,
		App:     fixtureApp,
		User:    testUser,
	}
}

func TestReleaseRefusesAnAppThatIsNotReserved(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	_, err = reservation.Release(testReleaseRequest(dir))
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsNotReserved(err) {
		t.Fatalf("expected a not-reserved error, got %v", err)
	}
	if !strings.Contains(err.Error(), fixtureApp) {
		t.Errorf("refusal does not name the app: %s", err.Error())
	}

	after, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if after.Hash() != before.Hash() {
		t.Errorf("a refused release changed the repo: %s -> %s", before.Hash(), after.Hash())
	}
}

// commitAt returns the commit object HEAD points to.
func commitAt(t *testing.T, repo *git.Repository) *object.Commit {
	t.Helper()

	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}

	return commit
}

// TestReleaseRestoresTheRepoByteForByte is the acceptance criterion with teeth:
// release has to undo Reserve so completely that the whole worktree, not just
// the files a reader would expect, comes back exactly as it was.
func TestReleaseRestoresTheRepoByteForByte(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := commitAt(t, repo)

	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}

	res, err := reservation.Release(testReleaseRequest(dir))
	if err != nil {
		t.Fatal(err)
	}
	if res.App != fixtureApp {
		t.Errorf("resolved app: got %q, want %q", res.App, fixtureApp)
	}

	after := commitAt(t, repo)
	if after.Hash == before.Hash {
		t.Fatal("release made no commit")
	}
	if res.Commit != after.Hash.String() {
		t.Errorf("result commit %q is not HEAD %q", res.Commit, after.Hash)
	}

	patch, err := before.Patch(after)
	if err != nil {
		t.Fatal(err)
	}
	if fp := patch.FilePatches(); len(fp) != 0 {
		t.Errorf("the worktree after release differs from before the reserve:\n%s", patch.String())
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
		t.Errorf("working tree is not clean after Release:\n%s", status)
	}
}

// TestReleaseRepeatedCyclesLeaveNothingBehind is user story 57: a cluster
// reserved and released several times in a row must come back byte for byte
// every time, not just the first.
func TestReleaseRepeatedCyclesLeaveNothingBehind(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := commitAt(t, repo)

	for i := 0; i < 3; i++ {
		if _, err := reservation.Reserve(testRequest(dir)); err != nil {
			t.Fatalf("cycle %d: reserve: %v", i, err)
		}
		if _, err := reservation.Release(testReleaseRequest(dir)); err != nil {
			t.Fatalf("cycle %d: release: %v", i, err)
		}

		after := commitAt(t, repo)
		patch, err := before.Patch(after)
		if err != nil {
			t.Fatal(err)
		}
		if fp := patch.FilePatches(); len(fp) != 0 {
			t.Errorf("cycle %d: worktree differs from the start:\n%s", i, patch.String())
		}
	}
}

// TestReleaseRestoresAPreexistingEmptyComponentsList covers the cluster whose
// collections/kustomization.yaml already carries `components: []` before its
// first reservation: addComponent turns that line into a real list, and
// release has to put `components: []` back rather than deleting the key or
// dropping whatever follows it, which byte-for-byte restoration would catch
// either way.
func TestReleaseRestoresAPreexistingEmptyComponentsList(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{
		collectionsKustomization: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  # Stands in for the remote management-cluster-bases collection stage.
  - ../../../bases/collections/demo
components: []
patches: []
`,
	})

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := commitAt(t, repo)

	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := reservation.Release(testReleaseRequest(dir)); err != nil {
		t.Fatal(err)
	}

	after := commitAt(t, repo)
	patch, err := before.Patch(after)
	if err != nil {
		t.Fatal(err)
	}
	if fp := patch.FilePatches(); len(fp) != 0 {
		t.Errorf("the worktree after release differs from before the reserve:\n%s", patch.String())
	}
}

// TestReleaseLeavesOtherReservationsIntact makes sure removing one app's
// component and ConfigMap entry does not disturb its neighbor: the
// components: list and the data: block both hold more than the one item
// Release has to delete.
func TestReleaseLeavesOtherReservationsIntact(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}
	other := testRequest(dir)
	other.App = fixtureOtherApp
	other.User = testOtherUser
	other.Branch = testOtherBranch
	if _, err := reservation.Reserve(other); err != nil {
		t.Fatal(err)
	}

	if _, err := reservation.Release(testReleaseRequest(dir)); err != nil {
		t.Fatal(err)
	}

	entries := reservationEntries(t, dir, fixtureCluster)
	if _, held := entries[fixtureApp]; held {
		t.Errorf("%s still holds a reservation after release", fixtureApp)
	}
	if got, want := entries[fixtureOtherApp]["user"], testOtherUser; got != want {
		t.Errorf("%s holder: got %q, want %q (release disturbed the other reservation)", fixtureOtherApp, got, want)
	}

	objects := renderCluster(t, dir, fixtureCluster)
	release := mustObject(t, objects, "HelmRelease/"+fixtureOtherApp)
	chartRef, _ := release["spec"].(map[string]any)["chartRef"].(map[string]any)
	if got, want := chartRef["name"], fixtureOtherApp+"-dev-reservation"; got != want {
		t.Errorf("%s chartRef name: got %v, want %q (release disturbed the other component)", fixtureOtherApp, got, want)
	}
}
