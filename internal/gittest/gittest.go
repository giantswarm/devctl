// Package gittest runs git for reservation test fixtures, in both
// cmd/reservation and pkg/reservation, failing the test on error.
package gittest

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// isolate keeps the developer's global and system git config (gpg signing, hooks,
// includes) out of the test, for the fixture's git and for devctl's own. A test
// that sets its own GIT_CONFIG_GLOBAL keeps it.
func isolate(t testing.TB) {
	t.Helper()

	// Git hooks export repository-local state. -C does not override these
	// variables, so fixture commands could otherwise use another repo's index,
	// config or objects. Empty paths have meaning to Git: unset them instead.
	for _, key := range []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_OBJECT_DIRECTORY",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
		"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE",
		"GIT_PREFIX", "GIT_SHALLOW_FILE", "GIT_COMMON_DIR",
	} {
		if _, ok := os.LookupEnv(key); !ok {
			continue
		}
		// Setenv registers cleanup and enforces testing's parallel-test guard;
		// unsetting afterward still restores the original value at cleanup.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}

	if os.Getenv("GIT_CONFIG_GLOBAL") == "" {
		t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// The fixtures are repos of a GitOps repo whose branch is main.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "init.defaultBranch")
	t.Setenv("GIT_CONFIG_VALUE_0", "main")
}

// RunGit runs git in dir, failing the test on error.
func RunGit(t testing.TB, dir string, args ...string) {
	t.Helper()
	isolate(t)

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
	isolate(t)

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out))
}
