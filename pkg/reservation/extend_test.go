package reservation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const testExtender = "reservation-extend"

// TestExtendRefusesWhenNothingMatches is the walking skeleton: an enabled
// cluster holding no reservation at all refuses, and the refusal names
// /deploy as the way to create one.
func TestExtendRefusesWhenNothingMatches(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsNothingToExtend(err) {
		t.Fatalf("expected a nothing-to-extend error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/deploy") {
		t.Errorf("refusal does not name /deploy: %s", err.Error())
	}
	if len(extended) != 0 {
		t.Errorf("got %d extended reservations, want 0: %+v", len(extended), extended)
	}
}

// TestExtendResetsTheExpiryOfAMatchingReservation is the first acceptance
// criterion: a single active reservation matching the pull request gets a
// fresh window that starts now and keeps its own stored duration, and the
// new window lands in the ConfigMap entry and is pushed.
func TestExtendResetsTheExpiryOfAMatchingReservation(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 4 * time.Hour
	reserveAndPush(t, dir, req)

	now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC) // before the original 14:00 expiry
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if len(extended) != 1 {
		t.Fatalf("got %d extended reservations, want 1: %+v", len(extended), extended)
	}

	got := extended[0]
	wantUntil := now.Add(4 * time.Hour) // the original 4h duration, not reset to the default
	if got.Cluster != fixtureCluster || got.App != fixtureApp || !got.From.Equal(now) || !got.Until.Equal(wantUntil) {
		t.Errorf("extended entry: %+v, want From %s Until %s", got, now, wantUntil)
	}

	entry := reservationEntries(t, dir, fixtureCluster)[fixtureApp]
	if got := entry["from"]; got != now.Format(time.RFC3339) {
		t.Errorf("entry from: got %q, want %q", got, now.Format(time.RFC3339))
	}
	if got := entry["until"]; got != wantUntil.Format(time.RFC3339) {
		t.Errorf("entry until: got %q, want %q", got, wantUntil.Format(time.RFC3339))
	}
	// Everything else about the reservation stays put.
	if got := entry["user"]; got != testUser {
		t.Errorf("entry user: got %q, want %q", got, testUser)
	}
	if got := entry["branch"]; got != testBranch {
		t.Errorf("entry branch: got %q, want %q", got, testBranch)
	}

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	tip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the extension did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}

// TestExtendIgnoresAnExpiredReservation is the MANDATORY rule: a record whose
// Until has already passed is treated as if it did not exist at all, even
// though it still names the pull request, because reviving it could silently
// break an exclusive lock someone else legally took over the same cluster
// while the dead record sat unswept. Extend refuses exactly as if it found
// nothing, and never pushes.
func TestExtendIgnoresAnExpiredReservation(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)
	beforeTip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))

	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC), // 1h past the 11:00 expiry
	})
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsNothingToExtend(err) {
		t.Fatalf("expected a nothing-to-extend error, got %v", err)
	}
	if len(extended) != 0 {
		t.Errorf("got %d extended reservations, want 0: %+v", len(extended), extended)
	}

	afterTip := gitOutput(t, origin, "rev-parse", gitOutput(t, dir, "branch", "--show-current"))
	if afterTip != beforeTip {
		t.Errorf("origin moved even though nothing was extended: %s -> %s", beforeTip, afterTip)
	}
}

// TestExtendResetsEveryMatchingReservation is the acceptance criterion "extend
// EVERY match, not just the first": a pull request holding reservations on two
// different apps on the same cluster gets both reset, not only the one List
// happens to return first.
func TestExtendResetsEveryMatchingReservation(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	first := testRequest(dir)
	first.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	first.Duration = 4 * time.Hour
	reserveAndPush(t, dir, first)

	second := testRequest(dir)
	second.App = fixtureOtherApp
	second.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	second.Duration = 2 * time.Hour
	reserveAndPush(t, dir, second)

	now := time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC) // before both expiries
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if len(extended) != 2 {
		t.Fatalf("got %d extended reservations, want 2: %+v", len(extended), extended)
	}

	byApp := map[string]reservation.Extended{}
	for _, e := range extended {
		byApp[e.App] = e
	}
	if got := byApp[fixtureApp]; !got.Until.Equal(now.Add(4 * time.Hour)) {
		t.Errorf("%s until: got %+v, want Until %s", fixtureApp, got, now.Add(4*time.Hour))
	}
	if got := byApp[fixtureOtherApp]; !got.Until.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("%s until: got %+v, want Until %s", fixtureOtherApp, got, now.Add(2*time.Hour))
	}

	entries := reservationEntries(t, dir, fixtureCluster)
	if entries[fixtureApp]["from"] != now.Format(time.RFC3339) {
		t.Errorf("%s entry not reset: %+v", fixtureApp, entries[fixtureApp])
	}
	if entries[fixtureOtherApp]["from"] != now.Format(time.RFC3339) {
		t.Errorf("%s entry not reset: %+v", fixtureOtherApp, entries[fixtureOtherApp])
	}
}
