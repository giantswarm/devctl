package reservation_test

import (
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestReserveIgnoresAnExpiredReservationOfTheSameApp is the "active" qualifier
// on rule 1: an expired entry the reaper has not swept yet must not block a
// new reservation of the same app.
func TestReserveIgnoresAnExpiredReservationOfTheSameApp(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	first := testRequest(dir)
	first.Duration = time.Hour
	if _, err := reservation.Reserve(first); err != nil {
		t.Fatal(err)
	}

	second := testRequest(dir)
	second.Now = first.Now.Add(2 * time.Hour) // well past the first's expiry
	second.User = testOtherUser
	second.Branch = testOtherBranch

	if _, err := reservation.Reserve(second); err != nil {
		t.Fatalf("an expired reservation blocked a new one: %v", err)
	}

	reservations, err := reservation.List(testListRequest(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 1 {
		t.Fatalf("got %d reservations, want 1: %+v", len(reservations), reservations)
	}
	if got := reservations[0].User; got != testOtherUser {
		t.Errorf("holder: got %q, want %q", got, testOtherUser)
	}
}
