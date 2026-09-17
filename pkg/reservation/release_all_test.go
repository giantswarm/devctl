package reservation_test

import (
	"context"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const testReleaser = "reservation-undeploy"

// TestReleaseAllReleasesTheReservationAPullRequestHolds is the walking
// skeleton: one reservation, one cluster, released by pull request alone --
// no cluster and no app argument, because a merged pull request knows
// neither.
func TestReleaseAllReleasesTheReservationAPullRequestHolds(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	reserveAndPush(t, dir, testRequest(dir))

	released, err := reservation.ReleaseAll(context.Background(), reservation.ReleaseAllRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testReleaser,
	})
	if err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
	if len(released) != 1 {
		t.Fatalf("got %d released reservations, want 1: %+v", len(released), released)
	}

	got := released[0]
	if got.Cluster != fixtureCluster || got.App != fixtureApp {
		t.Errorf("released entry: %+v, want cluster %q app %q", got, fixtureCluster, fixtureApp)
	}
	if got.User != testUser {
		t.Errorf("released user: got %q, want the original holder %q", got.User, testUser)
	}

	if entries := reservationEntries(t, dir, fixtureCluster); len(entries) != 0 {
		t.Errorf("the ConfigMap still holds %d entries, want 0: %+v", len(entries), entries)
	}

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	tip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the release did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}

// TestReleaseAllReleasesEveryAppOnOneCluster is the "every reservation it
// holds" criterion at its smallest: one pull request reserving two apps on
// one cluster loses both, not just the first one found.
func TestReleaseAllReleasesEveryAppOnOneCluster(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	first := testRequest(dir)
	reserveAndPush(t, dir, first)

	second := testRequest(dir)
	second.App = fixtureOtherApp
	reserveAndPush(t, dir, second)

	released, err := reservation.ReleaseAll(context.Background(), reservation.ReleaseAllRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testReleaser,
	})
	if err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
	if len(released) != 2 {
		t.Fatalf("got %d released reservations, want 2: %+v", len(released), released)
	}

	apps := map[string]bool{}
	for _, r := range released {
		apps[r.App] = true
	}
	if !apps[fixtureApp] || !apps[fixtureOtherApp] {
		t.Errorf("released apps: %+v, want both %q and %q", released, fixtureApp, fixtureOtherApp)
	}
	if entries := reservationEntries(t, dir, fixtureCluster); len(entries) != 0 {
		t.Errorf("the ConfigMap still holds %d entries, want 0: %+v", len(entries), entries)
	}
}
