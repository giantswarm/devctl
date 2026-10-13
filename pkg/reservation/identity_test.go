package reservation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// isolateGitConfig hides the global and system git config of whoever runs the tests.
func isolateGitConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// The identity comes from git itself, so an include.path counts; --user never does.
func TestReserveCommitsWithTheIdentityFromAnIncludedGitConfig(t *testing.T) {
	isolateGitConfig(t)
	dir := newGitOpsFixture(t, fixtureOptions{})
	runGit(t, dir, "config", "--unset", "user.name")
	runGit(t, dir, "config", "--unset", "user.email")
	inc := filepath.Join(t.TempDir(), "user")
	if err := os.WriteFile(inc, []byte("[user]\n\tname = Included Person\n\temail = included@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "config", "include.path", inc)

	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}
	want := "Included Person <included@example.com>"
	if got := runGit(t, dir, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != want+"|"+want {
		t.Errorf("author|committer: got %q, want %q", got, want+"|"+want)
	}
}

// Without an identity, reserve refuses before it writes anything.
func TestReserveWithoutGitIdentityLeavesTheCheckoutClean(t *testing.T) {
	isolateGitConfig(t)
	dir := newGitOpsFixture(t, fixtureOptions{})
	runGit(t, dir, "config", "--unset", "user.name")
	runGit(t, dir, "config", "--unset", "user.email")

	_, err := reservation.Reserve(testRequest(dir))
	if err == nil || !strings.Contains(err.Error(), "user.name and user.email are not set") {
		t.Fatalf("want an identity error, got %v", err)
	}
	if st := runGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("checkout is dirty:\n%s", st)
	}
}
