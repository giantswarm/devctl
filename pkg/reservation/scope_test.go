package reservation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestReserveRefusesAppScopedWhenExclusiveIsActive is rule 2: an exclusive
// reservation locks the whole cluster, so an unrelated app-scoped request
// fails against it even though the two never share an app.
func TestReserveRefusesAppScopedWhenExclusiveIsActive(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	exclusive := testRequest(dir)
	exclusive.Scope = reservation.ScopeExclusive
	if _, err := reservation.Reserve(exclusive); err != nil {
		t.Fatal(err)
	}

	appScoped := testRequest(dir)
	appScoped.App = fixtureOtherApp
	appScoped.User = testOtherUser
	appScoped.Branch = testOtherBranch

	_, err := reservation.Reserve(appScoped)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsClusterLocked(err) {
		t.Fatalf("expected a cluster-locked error, got %v", err)
	}
	// The refusal has to name the holder, the app, the branch and the expiry of
	// the exclusive reservation that blocks the request.
	for _, want := range []string{fixtureApp, testUser, testBranch, "2026-09-15T20:00:00Z"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}

// TestReserveRefusesExclusiveWhenAnotherAppIsReserved is rule 3: an exclusive
// request fails against any active reservation that is not its own sole one
// to promote.
func TestReserveRefusesExclusiveWhenAnotherAppIsReserved(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	first := testRequest(dir)
	if _, err := reservation.Reserve(first); err != nil {
		t.Fatal(err)
	}

	exclusive := testRequest(dir)
	exclusive.App = fixtureOtherApp
	exclusive.User = testOtherUser
	exclusive.Branch = testOtherBranch
	exclusive.Scope = reservation.ScopeExclusive

	_, err := reservation.Reserve(exclusive)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsClusterLocked(err) {
		t.Fatalf("expected a cluster-locked error, got %v", err)
	}
	if !strings.Contains(err.Error(), testUser) {
		t.Errorf("refusal does not name the holder: %s", err.Error())
	}
}

// TestReserveRefusesExclusiveWhenTwoOthersAreActive proves the promotion
// exception is narrow: it only fires when exactly one reservation is active,
// not merely when one of several belongs to the requester.
func TestReserveRefusesExclusiveWhenTwoOthersAreActive(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	mine := testRequest(dir)
	if _, err := reservation.Reserve(mine); err != nil {
		t.Fatal(err)
	}

	other := testRequest(dir)
	other.App = fixtureOtherApp
	other.User = testOtherUser
	other.Branch = testOtherBranch
	if _, err := reservation.Reserve(other); err != nil {
		t.Fatal(err)
	}

	exclusive := testRequest(dir)
	exclusive.Scope = reservation.ScopeExclusive // same user and app as `mine`

	_, err := reservation.Reserve(exclusive)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsClusterLocked(err) {
		t.Fatalf("expected a cluster-locked error, got %v", err)
	}
}

// TestReserveExclusivePromotesItsOwnSoleReservation is rule 3's exception: an
// exclusive request succeeds, as a promotion, when the one active reservation
// on the cluster belongs to the same user and the same app. It has to keep
// the reservation exactly as it was and rewrite its entry in place rather
// than removing and re-adding it.
func TestReserveExclusivePromotesItsOwnSoleReservation(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	first := testRequest(dir)
	if _, err := reservation.Reserve(first); err != nil {
		t.Fatal(err)
	}

	before := reservationEntries(t, dir, fixtureCluster)[fixtureApp]

	promotion := testRequest(dir)
	promotion.Scope = reservation.ScopeExclusive
	// A promotion request can even come with a different branch than the
	// original: it must be ignored, since the reservation it promotes keeps
	// everything but its scope.
	promotion.Branch = testOtherBranch

	res, err := reservation.Reserve(promotion)
	if err != nil {
		t.Fatalf("expected the promotion to succeed, got %v", err)
	}
	if got, want := res.Until, first.Now.Add(10*time.Hour); !got.Equal(want) {
		t.Errorf("promotion changed the expiry: got %s, want %s", got, want)
	}

	entries := reservationEntries(t, dir, fixtureCluster)
	if len(entries) != 1 {
		t.Fatalf("got %d reservation entries, want 1 (the entry must be rewritten in place, not duplicated): %+v", len(entries), entries)
	}

	after := entries[fixtureApp]
	if got, want := after["scope"], reservation.ScopeExclusive; got != want {
		t.Errorf("scope: got %q, want %q", got, want)
	}
	if got, want := after["branch"], before["branch"]; got != want {
		t.Errorf("promotion changed the branch: got %q, want %q (unchanged)", got, want)
	}
	if got, want := after["from"], before["from"]; got != want {
		t.Errorf("promotion changed `from`: got %q, want %q (unchanged)", got, want)
	}
	if got, want := after["until"], before["until"]; got != want {
		t.Errorf("promotion changed `until`: got %q, want %q (unchanged)", got, want)
	}
}
