package reservation_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// newReapFixture builds a GitOps repo fixture the way newGitOpsFixture does,
// pushes it to a local bare "origin", and returns a clone at dir: the
// checkout Reap operates on, exactly as a laptop checkout tracking a real
// remote would. origin lets a test assert what actually landed.
func newReapFixture(t *testing.T, opts fixtureOptions) (dir, origin string) {
	t.Helper()

	seed := newGitOpsFixture(t, opts)
	branch := gittest.GitOutput(t, seed, "branch", "--show-current")

	origin = t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", branch)
	gittest.RunGit(t, seed, "remote", "add", "origin", origin)
	gittest.RunGit(t, seed, "push", "-u", "origin", branch)

	dir = t.TempDir()
	gittest.RunGit(t, dir, "clone", origin, ".")

	return dir, origin
}

const testReaper = "reservation-reaper"

// reserveAndPush reserves req against dir and pushes the resulting commit, so
// dir starts even with its origin exactly like a laptop checkout that is up
// to date.
func reserveAndPush(t *testing.T, dir string, req reservation.Request) reservation.Result {
	t.Helper()

	result, err := reservation.Reserve(req)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := reservation.Push(context.Background(), dir); err != nil {
		t.Fatalf("pushing the fixture reservation: %v", err)
	}

	return result
}

// TestReapDoesNothingWhenNoReservations is the walking skeleton: an enabled
// cluster with no reservations at all reaps nothing and fails on nothing.
func TestReapDoesNothingWhenNoReservations(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("got %d reaped reservations, want 0: %+v", len(reaped), reaped)
	}
}

// TestReapReleasesAnExpiredReservation is the first acceptance criterion:
// Reap releases a reservation whose expiry passed, reports it, commits the
// release and pushes it -- with no remote push fixture standing in for a
// GitHub token, exactly as it would from a laptop.
func TestReapReleasesAnExpiredReservation(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC), // 1h past Until
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("got %d reaped reservations, want 1: %+v", len(reaped), reaped)
	}

	got := reaped[0]
	if got.Cluster != fixtureCluster || got.App != fixtureApp || got.User != testUser ||
		got.Branch != testBranch || got.PullRequest != testPullRequest || got.Reason != reservation.ReasonExpired {
		t.Errorf("reaped entry: %+v", got)
	}
	if got.Commit == "" {
		t.Error("reaped entry carries no commit hash")
	}

	if entries := reservationEntries(t, dir, fixtureCluster); entries[fixtureApp] != nil {
		t.Errorf("%s reservation still present after reaping: %+v", fixtureApp, entries[fixtureApp])
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the release did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}

// TestReapDoesNotDeleteAFreshReservationThatWonTheRace is the hard case: Reap
// reads dir's stale local state and decides the app's reservation is expired,
// but before its push lands a second clone reserves the very same app afresh
// and pushes first. Reap's push is rejected, PushWithRetry rebases dir onto
// the new tip and reruns render -- which must notice the record it is about
// to delete is no longer the expired one it decided to release, and leave the
// fresh reservation alone instead of deleting it under the old holder's name.
func TestReapDoesNotDeleteAFreshReservationThatWonTheRace(t *testing.T) {
	dir1, origin := newReapFixture(t, fixtureOptions{})

	// dir1 reserves and pushes the reservation that will have expired by the
	// time Reap runs.
	stale := testRequest(dir1)
	stale.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	stale.Duration = time.Hour
	reserveAndPush(t, dir1, stale)

	// dir2 clones the tip that still carries the stale reservation and
	// replaces it with a fresh one for the same app and cluster -- exactly
	// the developer who grabs the cluster the instant it frees up, before the
	// reaper gets around to it -- and lands it first.
	dir2 := t.TempDir()
	gittest.RunGit(t, dir2, "clone", origin, ".")
	fresh := testRequest(dir2)
	fresh.User = testOtherUser
	fresh.Branch = testOtherBranch
	fresh.Now = time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC)
	fresh.Duration = 10 * time.Hour
	reserveAndPush(t, dir2, fresh)

	// dir1 never fetched dir2's push: its local List still shows the stale,
	// expired reservation, so Reap decides to release it before it has any
	// idea a fresh one has taken its place on origin.
	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir1,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("got %d reaped reservations, want 0: the fresh reservation must survive uninvolved, not be deleted and reported as the stale one: %+v", len(reaped), reaped)
	}

	entries := reservationEntries(t, dir1, fixtureCluster)
	if got, want := entries[fixtureApp]["user"], testOtherUser; got != want {
		t.Errorf("%s holder: got %q, want %q (the fresh reservation that won the race must survive the reap)", fixtureApp, got, want)
	}
}

// TestReapReportsTheRecordItActuallyDeletedAfterARebase is the sibling of
// TestReapDoesNotDeleteAFreshReservationThatWonTheRace: the rebase's
// replacement reservation is also already expired by the time Reap runs, so
// the re-check inside render still finds a reason and the delete is correct.
// But reapCluster must report the record render actually deleted -- the
// replacement's holder, branch, pull request and expiry -- not the stale
// pre-push List read it started the sweep with.
func TestReapReportsTheRecordItActuallyDeletedAfterARebase(t *testing.T) {
	dir1, origin := newReapFixture(t, fixtureOptions{})

	// dir1 reserves and pushes the reservation that will have expired by the
	// time Reap runs.
	stale := testRequest(dir1)
	stale.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	stale.Duration = time.Hour
	reserveAndPush(t, dir1, stale)

	// dir2 clones the tip that still carries the stale reservation and
	// replaces it with a DIFFERENT reservation for the same app -- a
	// different holder, branch and pull request -- that has also already
	// expired by the time Reap runs, and lands it first.
	dir2 := t.TempDir()
	gittest.RunGit(t, dir2, "clone", origin, ".")
	replacement := testRequest(dir2)
	replacement.User = testOtherUser
	replacement.Branch = testOtherBranch
	replacement.PullRequest = "giantswarm/hello-world#456"
	replacement.Now = time.Date(2026, 9, 15, 11, 10, 0, 0, time.UTC) // after stale's Until (11:00), so it no longer blocks
	replacement.Duration = 15 * time.Minute                          // expires 11:25, still well before Reap's Now
	replacementResult := reserveAndPush(t, dir2, replacement)

	// dir1 never fetched dir2's push: its local List still shows the stale
	// reservation under the first holder's name, so Reap decides to release
	// it before it has any idea a different, also-expired reservation has
	// taken its place on origin.
	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir1,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("got %d reaped reservations, want 1: the replacement is also expired, so the re-check must still allow the delete: %+v", len(reaped), reaped)
	}

	got := reaped[0]
	if got.User != testOtherUser || got.Branch != testOtherBranch || got.PullRequest != replacement.PullRequest {
		t.Errorf("reaped entry: got %+v, want the record render actually deleted (user %q, branch %q, pr %q), not the stale pre-push read",
			got, testOtherUser, testOtherBranch, replacement.PullRequest)
	}
	if !got.Until.Equal(replacementResult.Until) {
		t.Errorf("reaped Until: got %s, want the replacement's own expiry %s", got.Until, replacementResult.Until)
	}
}

// constantHeadBranch stubs HeadBranchFunc: every pull request has the same
// current head branch, whatever the reservation stored.
func constantHeadBranch(branch string) reservation.HeadBranchFunc {
	return func(ctx context.Context, pullRequest string) (string, error) {
		return branch, nil
	}
}

// TestReapReleasesARenamedReservation is the second acceptance criterion: a
// reservation not yet expired is still released when its pull request's
// branch was renamed out from under it, because the old branch makes no more
// builds.
func TestReapReleasesARenamedReservation(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 10 * time.Hour // still active at the Now below
	reserveAndPush(t, dir, req)

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir:    dir,
		User:       testReaper,
		Now:        time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		HeadBranch: constantHeadBranch("fix/renamed"),
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("got %d reaped reservations, want 1: %+v", len(reaped), reaped)
	}
	if got := reaped[0].Reason; got != reservation.ReasonRenamed {
		t.Errorf("Reason: got %q, want %q", got, reservation.ReasonRenamed)
	}
}

// TestReapLeavesAnActiveUnrenamedReservationAlone is the fourth acceptance
// criterion, "stays silent when it finds nothing": a reservation that is
// neither expired nor renamed is not released, and Reap never even pushes.
func TestReapLeavesAnActiveUnrenamedReservationAlone(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 10 * time.Hour
	reserveAndPush(t, dir, req)
	beforeTip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir:    dir,
		User:       testReaper,
		Now:        time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		HeadBranch: constantHeadBranch(testBranch), // unchanged
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 0 {
		t.Errorf("got %d reaped reservations, want 0: %+v", len(reaped), reaped)
	}

	afterTip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if afterTip != beforeTip {
		t.Errorf("origin moved even though nothing was reaped: %s -> %s", beforeTip, afterTip)
	}
}

// TestReapSkipsAClusterThatNeverOptedIn is the fifth acceptance criterion's
// other half: a management cluster the GitOps repo holds but that carries no
// reservations ConfigMap is skipped, not a failure that stops the sweep.
func TestReapSkipsAClusterThatNeverOptedIn(t *testing.T) {
	const notEnabled = "not-enabled-cluster"

	dir, _ := newReapFixture(t, fixtureOptions{})

	// A cluster directory with no configmap-reservations.yaml: never enabled.
	// It need not be committed -- Reap reads the working tree directly, exactly
	// as List and Release do.
	notEnabledDir := dir + "/management-clusters/" + notEnabled
	if err := os.MkdirAll(notEnabledDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notEnabledDir+"/kustomization.yaml", []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 1 {
		t.Fatalf("got %d reaped reservations, want 1 (the enabled cluster's): %+v", len(reaped), reaped)
	}
	if reaped[0].Cluster != fixtureCluster {
		t.Errorf("reaped cluster: got %q, want %q", reaped[0].Cluster, fixtureCluster)
	}
}

// TestReapContinuesAfterABrokenCluster is the sixth acceptance criterion: one
// cluster whose ConfigMap fails to parse costs its own error, not the sweep --
// another cluster's overdue reservation is still released and pushed, and the
// broken cluster's failure comes back for the caller to report.
func TestReapContinuesAfterABrokenCluster(t *testing.T) {
	const brokenCluster = "broken-cluster"

	dir, origin := newReapFixture(t, fixtureOptions{clusters: []string{fixtureCluster, brokenCluster}})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)

	// Corrupt the broken cluster's ConfigMap so List fails on it. It need not be
	// committed -- Reap reads the working tree directly.
	brokenConfigMap := filepath.Join(dir, "management-clusters", brokenCluster, "configmap-reservations.yaml")
	corrupt := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"data:\n" +
		"  broken-app: '{user: mallory, branch: x, pr: \"\", scope: app, from: not-a-date, until: not-a-date}'\n"
	if err := os.WriteFile(brokenConfigMap, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected an error reporting the broken cluster, got none")
	}
	if len(reaped) != 1 || reaped[0].Cluster != fixtureCluster {
		t.Fatalf("got %+v, want the enabled cluster's release despite the broken one", reaped)
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the good cluster's release did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}
