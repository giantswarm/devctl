package reservation_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// runGit runs git in dir for a push test fixture, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitOutput runs git in dir and returns its trimmed stdout, failing the test
// on error.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out))
}

// newPushFixture builds a bare "origin" holding one commit on main, and a
// clone of it at dir, so a test can commit into dir and push it for real.
func newPushFixture(t *testing.T) (dir, origin string) {
	t.Helper()

	origin = t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")

	seed := t.TempDir()
	runGit(t, seed, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "-A")
	runGit(t, seed, "commit", "-m", "seed")
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "-u", "origin", "main")

	dir = t.TempDir()
	runGit(t, dir, "clone", origin, ".")

	return dir, origin
}

// writeAndCommit writes name in dir with content and commits it, the way a
// real render callback (Reserve or Release) commits its own change.
func writeAndCommit(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "add "+name)
}

// TestPushWithRetrySucceedsOnFirstAttempt checks the plain case: dir is
// already even with origin, so render runs once and the push lands with no
// rebase involved.
func TestPushWithRetrySucceedsOnFirstAttempt(t *testing.T) {
	dir, origin := newPushFixture(t)

	calls := 0
	render := func() error {
		calls++
		writeAndCommit(t, dir, "change.txt", "change\n")
		return nil
	}

	if err := reservation.PushWithRetry(context.Background(), dir, "main", render); err != nil {
		t.Fatalf("PushWithRetry: %v", err)
	}
	if calls != 1 {
		t.Errorf("render called %d times, want 1", calls)
	}

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	tip := gitOutput(t, origin, "rev-parse", "main")
	if head != tip {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, tip)
	}
}
