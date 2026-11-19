package reservation_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestExtendWritesWhatReserveWould: an extended reservation reads exactly like a
// fresh reserve by the extender at the same moment, so the quoting of an
// annotation never depends on which command wrote it last.
func TestExtendWritesWhatReserveWould(t *testing.T) {
	for name, request := range map[string]func(string) reservation.Request{
		"collections": testRequest,
		"extras":      extrasRequest,
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
			now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC)

			extendedDir, _ := newReapFixture(t, fixtureOptions{})
			req := request(extendedDir)
			req.Now, req.Duration = start, 4*time.Hour
			reserveAndPush(t, extendedDir, req)
			if _, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
				RepoDir:     extendedDir,
				PullRequest: req.PullRequest,
				User:        testExtender,
				Now:         now,
			}); err != nil {
				t.Fatalf("Extend: %v", err)
			}

			reservedDir, _ := newReapFixture(t, fixtureOptions{})
			fresh := request(reservedDir)
			fresh.Now, fresh.Duration, fresh.User = now, 4*time.Hour, testExtender
			reserveAndPush(t, reservedDir, fresh)

			_, files := headFiles(t, extendedDir)
			for _, f := range files {
				got, err := os.ReadFile(filepath.Join(extendedDir, f)) //nolint:gosec // a test fixture
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(filepath.Join(reservedDir, f)) //nolint:gosec // a test fixture
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Errorf("%s after extend:\n%s\nwant, as a fresh reserve writes it:\n%s", f, got, want)
				}
			}
		})
	}
}
