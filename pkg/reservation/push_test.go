package reservation_test

import (
	"context"
	"fmt"
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

	if err := reservation.PushWithRetry(context.Background(), dir, render); err != nil {
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

// TestPushWithRetryRerendersAfterAnotherReservationLands proves the ticket's
// hard case: a rejected push must not just replay the stale commit render
// already made. It fetches, resets to the new tip, and reruns Reserve from
// there, so a reservation that landed first is folded in rather than
// corrupted or lost.
func TestPushWithRetryRerendersAfterAnotherReservationLands(t *testing.T) {
	seed := newGitOpsFixture(t, fixtureOptions{})
	branch := gitOutput(t, seed, "branch", "--show-current")

	origin := t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", branch)
	runGit(t, seed, "remote", "add", "origin", origin)
	runGit(t, seed, "push", "-u", "origin", branch)

	dir1 := t.TempDir()
	runGit(t, dir1, "clone", origin, ".")
	dir2 := t.TempDir()
	runGit(t, dir2, "clone", origin, ".")

	// dir2 reserves other-app and lands first, exactly as a second, unrelated
	// pull request's reservation would.
	req2 := testRequest(dir2)
	req2.App = fixtureOtherApp
	req2.User = testOtherUser
	req2.Branch = testOtherBranch
	if _, err := reservation.Reserve(req2); err != nil {
		t.Fatalf("reserving other-app: %v", err)
	}
	if err := reservation.Push(context.Background(), dir2); err != nil {
		t.Fatalf("pushing other-app: %v", err)
	}

	// dir1 started from the same tip as dir2 and still reserves hello-world
	// against it: its first push is rejected.
	calls := 0
	req1 := testRequest(dir1)
	render := func() error {
		calls++
		_, err := reservation.Reserve(req1)
		return err
	}

	if err := reservation.PushWithRetry(context.Background(), dir1, render); err != nil {
		t.Fatalf("PushWithRetry: %v", err)
	}
	if calls != 2 {
		t.Errorf("render called %d times, want 2 (the rejected attempt and the rebased retry)", calls)
	}

	head := gitOutput(t, dir1, "rev-parse", "HEAD")
	tip := gitOutput(t, origin, "rev-parse", branch)
	if head != tip {
		t.Errorf("push did not land: local HEAD %s, origin %s %s", head, branch, tip)
	}

	entries := reservationEntries(t, dir1, fixtureCluster)
	if got, want := entries[fixtureApp]["user"], testUser; got != want {
		t.Errorf("%s holder: got %q, want %q", fixtureApp, got, want)
	}
	if got, want := entries[fixtureOtherApp]["user"], testOtherUser; got != want {
		t.Errorf("other-app holder: got %q, want %q (the reservation that landed first must survive the rebase)", got, want)
	}
}

// TestPushWithRetryRefusesToRebaseADirtyWorktree is the regression for a HIGH
// finding: release, reap and extend all default --repo-dir to ".", running
// directly against a developer's own checkout rather than a clone. A rejected
// push must never fall back to blindly hard-resetting that checkout, because
// `git reset --hard` cannot spare a file it never wrote -- it would destroy
// whatever unrelated, uncommitted work the developer had in progress there.
func TestPushWithRetryRefusesToRebaseADirtyWorktree(t *testing.T) {
	dir, origin := newPushFixture(t)
	saboteur := t.TempDir()
	runGit(t, saboteur, "clone", origin, ".")

	const unrelated = "wip.txt"
	const wipContent = "someone else's in-progress edit\n"
	if err := os.WriteFile(filepath.Join(dir, unrelated), []byte(wipContent), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	render := func() error {
		calls++
		// commitAll only ever stages its own files, never -A, so render's own
		// commit here must do the same: staying oblivious to wip.txt is exactly
		// what leaves it dirty for rebaseOntoRemote to catch.
		if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte(fmt.Sprintf("change %d\n", calls)), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "change.txt")
		runGit(t, dir, "commit", "-m", "add change.txt")

		if calls == 1 {
			// Someone else's push lands first, so dir's own push is rejected and
			// PushWithRetry must rebase before it may render again.
			writeAndCommit(t, saboteur, "sabotage.txt", "sabotage\n")
			runGit(t, saboteur, "push", "origin", "main")
		}

		return nil
	}

	err := reservation.PushWithRetry(context.Background(), dir, render)
	if err == nil {
		t.Fatal("expected a failure, got none")
	}
	if !reservation.IsDirtyWorktree(err) {
		t.Fatalf("expected a dirty-worktree error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("render called %d times, want 1: the rebase must be refused before a second render", calls)
	}

	got, err := os.ReadFile(filepath.Join(dir, unrelated))
	if err != nil {
		t.Fatalf("the unrelated file was removed from the working tree: %v", err)
	}
	if string(got) != wipContent {
		t.Errorf("unrelated file content: got %q, want %q", got, wipContent)
	}
}

// TestPushWithRetryGivesUpAfterMaxAttempts checks the ceiling: a push that
// loses the race every single time stops after MaxPushAttempts and reports
// the failure, instead of retrying forever.
func TestPushWithRetryGivesUpAfterMaxAttempts(t *testing.T) {
	dir, origin := newPushFixture(t)
	saboteur := t.TempDir()
	runGit(t, saboteur, "clone", origin, ".")

	calls := 0
	render := func() error {
		calls++
		writeAndCommit(t, dir, "change.txt", fmt.Sprintf("change %d\n", calls))

		// However dir's push turns out, someone else's push always lands first,
		// so dir's is rejected again on every attempt.
		runGit(t, saboteur, "pull", "--rebase", "origin", "main")
		writeAndCommit(t, saboteur, "sabotage.txt", fmt.Sprintf("sabotage %d\n", calls))
		runGit(t, saboteur, "push", "origin", "main")

		return nil
	}

	err := reservation.PushWithRetry(context.Background(), dir, render)
	if err == nil {
		t.Fatal("expected a failure, got none")
	}
	if !reservation.IsPushRetriesExhausted(err) {
		t.Fatalf("expected a push-retries-exhausted error, got %v", err)
	}
	if calls != reservation.MaxPushAttempts {
		t.Errorf("render called %d times, want %d", calls, reservation.MaxPushAttempts)
	}
}
