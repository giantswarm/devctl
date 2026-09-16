package reservation_test

import (
	"context"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// newReapFixture builds a GitOps repo fixture the way newGitOpsFixture does,
// pushes it to a local bare "origin", and returns a clone at dir: the
// checkout Reap operates on, exactly as a laptop checkout tracking a real
// remote would. origin lets a test assert what actually landed.
func newReapFixture(t *testing.T, opts fixtureOptions) (dir, origin string) {
	t.Helper()

	seed := newGitOpsFixture(t, opts)
	branch := gitOutput(t, seed, "branch", "--show-current")

	origin = t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", branch)
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "-u", "origin", branch)

	dir = t.TempDir()
	runGit(t, dir, "clone", origin, ".")

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

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	tip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the release did not land on origin: local HEAD %s, origin %s", head, tip)
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
	beforeTip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))

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

	afterTip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))
	if afterTip != beforeTip {
		t.Errorf("origin moved even though nothing was reaped: %s -> %s", beforeTip, afterTip)
	}
}
