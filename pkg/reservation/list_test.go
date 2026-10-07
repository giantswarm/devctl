package reservation_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func testListRequest(dir string) reservation.ListRequest {
	return reservation.ListRequest{
		RepoDir: dir,
		Cluster: fixtureCluster,
	}
}

// TestListReturnsTheActiveReservationsFields is user story 26: list has to
// print exactly what reservationEntry wrote, never anything reconstructed
// from elsewhere.
func TestListReturnsTheActiveReservationsFields(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	reserved, err := reservation.Reserve(testRequest(dir))
	if err != nil {
		t.Fatal(err)
	}

	reservations, err := reservation.List(testListRequest(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 1 {
		t.Fatalf("got %d reservations, want 1: %+v", len(reservations), reservations)
	}

	want := reservation.Reservation{
		App:         fixtureApp,
		User:        testUser,
		Branch:      testBranch,
		PullRequest: testPullRequest,
		Scope:       reservation.ScopeApp,
		From:        reserved.From,
		Until:       reserved.Until,
	}
	if diff := cmp.Diff(want, reservations[0]); diff != "" {
		t.Errorf("reservation mismatch (-want +got):\n%s", diff)
	}
}

// TestListSortsSeveralReservationsByApp makes list's output deterministic:
// two callers of a command that just prints a slice must see the same order.
func TestListSortsSeveralReservationsByApp(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	// fixtureOtherApp ("other-app") sorts after fixtureApp ("hello-world")
	// alphabetically, but is reserved first, so a passing test rules out
	// "sorted by insertion order" along with "unsorted".
	other := testRequest(dir)
	other.App = fixtureOtherApp
	other.User = testOtherUser
	other.Branch = testOtherBranch
	if _, err := reservation.Reserve(other); err != nil {
		t.Fatal(err)
	}
	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}

	reservations, err := reservation.List(testListRequest(dir))
	if err != nil {
		t.Fatal(err)
	}

	var apps []string
	for _, r := range reservations {
		apps = append(apps, r.App)
	}
	want := []string{fixtureApp, fixtureOtherApp}
	if diff := cmp.Diff(want, apps); diff != "" {
		t.Errorf("app order mismatch (-want +got):\n%s", diff)
	}
}

// TestListRefusesClusterThatIsNotEnabled matches Reserve's and Release's
// refusal: the same cluster-not-enabled error, not a bare file-not-found.
func TestListRefusesClusterThatIsNotEnabled(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{omitConfigMap: true})

	_, err := reservation.List(testListRequest(dir))
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsClusterNotEnabled(err) {
		t.Fatalf("expected a cluster-not-enabled error, got %v", err)
	}
}

// TestListReturnsNoneOnAClusterWithNoReservations covers the cluster that is
// enabled but currently free: an empty slice, not an error.
func TestListReturnsNoneOnAClusterWithNoReservations(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	reservations, err := reservation.List(testListRequest(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 0 {
		t.Errorf("got %d reservations, want 0: %+v", len(reservations), reservations)
	}
}
