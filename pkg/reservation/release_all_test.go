package reservation_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
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

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
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

// TestReleaseAllContinuesAfterABrokenCluster matches how Reap and Extend
// behave: one cluster whose ConfigMap fails to parse costs its own error, not
// the sweep. A merged pull request gets one chance to clean up, so a cluster
// nobody can read must not strand the reservations on every other cluster.
func TestReleaseAllContinuesAfterABrokenCluster(t *testing.T) {
	const brokenCluster = "broken-cluster" // sorts before fixtureCluster, so the sweep meets it first

	dir, origin := newReapFixture(t, fixtureOptions{clusters: []string{fixtureCluster, brokenCluster}})

	reserveAndPush(t, dir, testRequest(dir))

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

	released, err := reservation.ReleaseAll(context.Background(), reservation.ReleaseAllRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testReleaser,
	})
	if err == nil {
		t.Fatal("expected the broken cluster's failure to come back, got none")
	}
	if !strings.Contains(err.Error(), brokenCluster) {
		t.Errorf("the error does not name the broken cluster: %s", err.Error())
	}
	if len(released) != 1 {
		t.Fatalf("got %d released reservations, want the healthy cluster's 1: %+v", len(released), released)
	}
	if released[0].Cluster != fixtureCluster {
		t.Errorf("released cluster: got %q, want %q", released[0].Cluster, fixtureCluster)
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the healthy cluster's release did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}

// TestReleaseRefusesAnotherPullRequestsReservation is the ticket's
// authorization rule: a release that names a pull request may only remove
// what that pull request holds. Without it, /undeploy <MC> from any pull
// request in the repo frees a cluster somebody else is testing on.
func TestReleaseRefusesAnotherPullRequestsReservation(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	reserveAndPush(t, dir, testRequest(dir))

	_, err := reservation.Release(reservation.ReleaseRequest{
		RepoDir:     dir,
		Cluster:     fixtureCluster,
		App:         fixtureApp,
		User:        testOtherUser,
		PullRequest: "giantswarm/hello-world#999",
	})
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsNotReserved(err) {
		t.Fatalf("expected a not-reserved error, got %v", err)
	}

	if entries := reservationEntries(t, dir, fixtureCluster); len(entries) != 1 {
		t.Errorf("the refusal removed the reservation: %+v", entries)
	}
}

// TestReleaseAllReleasesOnEveryCluster locks in the cross-cluster sweep: a
// branch tested against two providers holds a reservation on each, and a
// merged pull request loses both.
func TestReleaseAllReleasesOnEveryCluster(t *testing.T) {
	const secondCluster = "iridium"

	dir, _ := newReapFixture(t, fixtureOptions{clusters: []string{fixtureCluster, secondCluster}})

	first := testRequest(dir)
	reserveAndPush(t, dir, first)

	second := testRequest(dir)
	second.Cluster = secondCluster
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

	for _, cluster := range []string{fixtureCluster, secondCluster} {
		if entries := reservationEntries(t, dir, cluster); len(entries) != 0 {
			t.Errorf("%s still holds %d entries, want 0: %+v", cluster, len(entries), entries)
		}
	}
}

// TestReleaseAllLeavesAnotherPullRequestsReservationAlone locks in the
// authorization rule at the sweep level: a merged pull request cleans up
// after itself only.
func TestReleaseAllLeavesAnotherPullRequestsReservationAlone(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	reserveAndPush(t, dir, testRequest(dir))

	released, err := reservation.ReleaseAll(context.Background(), reservation.ReleaseAllRequest{
		RepoDir:     dir,
		PullRequest: "giantswarm/hello-world#999",
		User:        testReleaser,
	})
	if err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
	if len(released) != 0 {
		t.Fatalf("got %d released reservations, want 0: %+v", len(released), released)
	}
	if entries := reservationEntries(t, dir, fixtureCluster); len(entries) != 1 {
		t.Errorf("the other pull request's reservation went: %+v", entries)
	}
}

// TestReleaseAllSaysNothingWhenThePullRequestHoldsNothing locks in the
// difference from Extend: a merged pull request that reserved nothing is the
// ordinary case, not a mistake, so it is no error. Only /undeploy tells the
// user, and it does that from an empty result.
func TestReleaseAllSaysNothingWhenThePullRequestHoldsNothing(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	released, err := reservation.ReleaseAll(context.Background(), reservation.ReleaseAllRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testReleaser,
	})
	if err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
	if len(released) != 0 {
		t.Fatalf("got %d released reservations, want 0: %+v", len(released), released)
	}
}

// TestReleaseAllReleasesAnExpiredReservation locks in the other deliberate
// difference from Extend, which skips an expired record: releasing one is
// plain cleanup that the reaper would do anyway, and leaving it behind would
// keep a dead entry in the ConfigMap after the pull request merged.
func TestReleaseAllReleasesAnExpiredReservation(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // long expired by any real clock
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)

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
	if entries := reservationEntries(t, dir, fixtureCluster); len(entries) != 0 {
		t.Errorf("the expired entry survived: %+v", entries)
	}
}
