package reservation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestReserveExclusiveDoesNotLockOtherClusters is rule 2/3's cluster
// boundary: an exclusive reservation locks its own cluster, never any other
// one, so the same app stays reservable elsewhere.
func TestReserveExclusiveDoesNotLockOtherClusters(t *testing.T) {
	const otherCluster = "gauss"

	dir := newGitOpsFixture(t, fixtureOptions{clusters: []string{fixtureCluster, otherCluster}})

	exclusive := testRequest(dir)
	exclusive.Scope = reservation.ScopeExclusive
	if _, err := reservation.Reserve(exclusive); err != nil {
		t.Fatal(err)
	}

	elsewhere := testRequest(dir)
	elsewhere.Cluster = otherCluster
	elsewhere.User = testOtherUser
	elsewhere.Branch = testOtherBranch
	if _, err := reservation.Reserve(elsewhere); err != nil {
		t.Fatalf("an exclusive reservation on %s blocked a request on %s: %v", fixtureCluster, otherCluster, err)
	}

	if got := reservationEntries(t, dir, otherCluster)[fixtureApp]["user"]; got != testOtherUser {
		t.Errorf("%s holder on %s: got %q, want %q", fixtureApp, otherCluster, got, testOtherUser)
	}
}

// TestReserveExclusivePatchesOnlyItsOwnApp is criterion 9: an exclusive
// reservation is a lock, not a cluster-wide patch. Every other app on the
// cluster must render exactly as it did before.
func TestReserveExclusivePatchesOnlyItsOwnApp(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	before := renderCluster(t, dir, fixtureCluster)

	exclusive := testRequest(dir)
	exclusive.Scope = reservation.ScopeExclusive
	res, err := reservation.Reserve(exclusive)
	if err != nil {
		t.Fatal(err)
	}

	after := renderCluster(t, dir, fixtureCluster)

	release := mustObject(t, after, "HelmRelease/"+fixtureApp)
	chartRef, _ := release["spec"].(map[string]any)["chartRef"].(map[string]any)
	if got := chartRef["name"]; got != res.SourceName {
		t.Errorf("%s HelmRelease chartRef name: got %v, want %q", fixtureApp, got, res.SourceName)
	}

	for _, key := range []string{
		"OCIRepository/" + fixtureOtherApp,
		"HelmRelease/" + fixtureOtherApp,
	} {
		if diff := cmp.Diff(mustObject(t, before, key), mustObject(t, after, key)); diff != "" {
			t.Errorf("%s changed even though the reservation is exclusive to %s (-before +after):\n%s", key, fixtureApp, diff)
		}
	}
}
