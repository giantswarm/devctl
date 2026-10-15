// Package gittest runs git for reservation test fixtures, in both
// cmd/reservation and pkg/reservation, failing the test on error.
package gittest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// RunGit runs git in dir, failing the test on error.
func RunGit(t testing.TB, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	// A fresh repo gets a local identity, so devctl's commits pass on a machine
	// with no global one.
	if args[0] == "init" || args[0] == "clone" {
		RunGit(t, dir, "config", "user.name", "test")
		RunGit(t, dir, "config", "user.email", "test@example.com")
	}
}

// GitOutput runs git in dir and returns its trimmed stdout, failing the test
// on error.
func GitOutput(t testing.TB, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out))
}
