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
