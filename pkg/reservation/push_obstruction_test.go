package reservation_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestPushWithRetryPreservesUntrackedObstructions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		local   string
		remote  string
		ignored bool
	}{
		{"case 0: file", "notes.txt", "notes.txt", false},
		{"case 1: directory replaced by file", "notes/contents.txt", "notes", false},
		{"case 2: file replaced by directory", "notes", "notes/contents.txt", false},
		{"case 3: ignored file", "notes.txt", "notes.txt", true},
		{"case 4: nonconflicting file", "notes.txt", "other.txt", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, origin := newPushFixture(t)
			competitor := t.TempDir()
			gittest.RunGit(t, competitor, "clone", origin, ".")
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, tc.local)), 0o755); err != nil { //nolint:gosec // temporary fixture
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.local), []byte("local work\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.ignored {
				if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte(tc.local+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var localCommit, remoteCommit string
			calls := 0
			err := reservation.PushWithRetry(context.Background(), dir, false, func() error {
				calls++
				if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte(fmt.Sprintf("attempt %d\n", calls)), 0o600); err != nil {
					t.Fatal(err)
				}
				gittest.RunGit(t, dir, "add", "change.txt")
				gittest.RunGit(t, dir, "commit", "-m", "reservation")
				localCommit = gittest.GitOutput(t, dir, "rev-parse", "HEAD")
				if calls == 1 {
					if err := os.MkdirAll(filepath.Dir(filepath.Join(competitor, tc.remote)), 0o755); err != nil { //nolint:gosec // temporary fixture
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(competitor, tc.remote), []byte("remote work\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					gittest.RunGit(t, competitor, "add", tc.remote)
					gittest.RunGit(t, competitor, "commit", "-m", "competing commit")
					gittest.RunGit(t, competitor, "push", "origin", "HEAD:main")
					remoteCommit = gittest.GitOutput(t, origin, "rev-parse", "main")
				}
				return nil
			})
			if tc.name == "case 4: nonconflicting file" {
				if err != nil || calls != 2 {
					t.Fatalf("want successful retry, calls=%d err=%v", calls, err)
				}
				if gittest.GitOutput(t, dir, "rev-parse", "HEAD") != gittest.GitOutput(t, origin, "rev-parse", "main") {
					t.Fatal("retry was not pushed")
				}
			} else {
				if !reservation.IsPushFailed(err) || !strings.Contains(err.Error(), "notes") {
					t.Fatalf("want refusal naming obstruction: %v", err)
				}
				if calls != 1 {
					t.Fatalf("render called %d times after obstruction", calls)
				}
				if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != localCommit {
					t.Fatal("reservation commit was discarded")
				}
				if got := gittest.GitOutput(t, origin, "rev-parse", "main"); got != remoteCommit {
					t.Fatal("origin changed after refusal")
				}
			}
			if got := readFixtureFile(t, dir, tc.local); got != "local work\n" {
				t.Fatalf("user file changed: %q", got)
			}
		})
	}
}
