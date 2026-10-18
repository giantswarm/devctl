package changelog

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const changelogHead = "# Changelog\n\n## [Unreleased]\n\n### Fixed\n\n"

// Test_CheckPendingCut builds the state a release leaves behind on Auto
// Release: main's merge of "Old." cut v1.1.0 and its cut pull request has not
// merged, so main's CHANGELOG.md still lists "Old." under Unreleased. An open
// pull request adding "New." is checked on its merge with main.
func Test_CheckPendingCut(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...) // #nosec G204 -- fixed binary, args are built by this test, test-only
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n\n- First.\n")
	git("add", ".")
	git("commit", "-q", "-m", "feat: first")
	git("tag", "v1.0.0")
	git("checkout", "-q", "-b", "pr")
	git("checkout", "-q", "main")
	write(changelogHead + "- Old.\n\n## [1.0.0] - 2026-01-01\n\n- First.\n")
	git("commit", "-q", "-am", "fix: old")
	git("tag", "v1.1.0") // the release; its cut pull request is still open
	base := git("rev-parse", "HEAD")

	// The open pull request, branched before the release, adds "New.".
	git("checkout", "-q", "pr")
	write(changelogHead + "- New.\n\n## [1.0.0] - 2026-01-01\n\n- First.\n")
	git("commit", "-q", "-am", "fix: new")
	// Its merge with main, as the pull_request event checks it out.
	git("checkout", "-q", "--detach", "main")
	write(changelogHead + "- New.\n- Old.\n\n## [1.0.0] - 2026-01-01\n\n- First.\n")
	git("add", ".")
	git("commit", "-q", "-m", "merge pr into main")
	merge := git("rev-parse", "HEAD")
	t.Chdir(dir)

	check := func(args ...string) (string, error) {
		t.Helper()
		var out bytes.Buffer
		c, err := New(Config{Stdout: &out, Stderr: &out})
		if err != nil {
			t.Fatal(err)
		}
		c.SetArgs(append([]string{"check"}, args...))
		c.SetOut(&out)
		c.SetErr(&out)
		c.SilenceUsage = true
		err = c.Execute()
		return out.String(), err
	}

	// Without the base, the pull request carries main's pending cut and fails.
	if _, err := check(); !IsReleasedEntries(err) {
		t.Fatalf("check without --base: err = %v, want released entries", err)
	}

	// With it, main's released entry waits for main's cut and passes.
	out, err := check("--base", base)
	if err != nil {
		t.Fatalf("check --base: err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "wait for its changelog cut pull request") || !strings.Contains(out, "v1.1.0: - Old.") {
		t.Fatalf("check --base does not list the pending entry:\n%s", out)
	}

	// The cut pull request itself: main's cut, merged with main, passes.
	git("checkout", "-q", "--detach", base)
	write("# Changelog\n\n## [Unreleased]\n\n## [1.1.0] - 2026-01-01\n\n### Fixed\n\n- Old.\n\n## [1.0.0] - 2026-01-01\n\n- First.\n")
	git("commit", "-q", "-am", "docs(changelog): cut")
	if out, err := check("--base", base); err != nil {
		t.Fatalf("the cut pull request: err = %v\n%s", err, out)
	}

	// A pull request that brings a released entry back after main's cut still
	// fails: the entry is its own, not main's.
	cut := git("rev-parse", "HEAD")
	write(changelogHead + "- Old.\n\n## [1.1.0] - 2026-01-01\n\n### Fixed\n\n- Old.\n\n## [1.0.0] - 2026-01-01\n\n- First.\n")
	git("commit", "-q", "-am", "fix: stale branch")
	if _, err := check("--base", cut); !IsReleasedEntries(err) {
		t.Fatalf("a pull request re-adding a released entry: err = %v, want released entries", err)
	}

	// A base the clone does not have fails, saying so.
	git("checkout", "-q", "--detach", merge)
	if _, err := check("--base", "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("check with an unknown base passed")
	}
}
