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
