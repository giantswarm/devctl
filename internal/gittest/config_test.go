package gittest_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

func TestInheritedGitConfigParameters(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	// Git passes -c settings to hooks and shell aliases through this variable.
	for name, parameters := range map[string]string{
		"valid":     "'init.defaultBranch=from-alias' 'commit.gpgsign=true'",
		"malformed": "'unclosed",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GIT_CONFIG_PARAMETERS", parameters)
			t.Run("RunGit", func(t *testing.T) {
				dir := t.TempDir()
				gittest.RunGit(t, dir, "init")
				if got := gittest.GitOutput(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
					t.Errorf("fixture branch = %q, want main", got)
				}
				gittest.RunGit(t, dir, "commit", "--allow-empty", "-m", "fixture")
				// devctl's Git children must inherit the same isolation.
				if got := gitAfterConfigIsolation(t, dir, "config", "--get", "init.defaultBranch"); got != "main" {
					t.Errorf("later Git child default branch = %q, want main", got)
				}
			})
			if os.Getenv("GIT_CONFIG_PARAMETERS") != parameters {
				t.Error("RunGit did not restore the caller's parameters at test cleanup")
			}

			t.Run("GitOutput", func(t *testing.T) {
				dir := t.TempDir()
				if got := gittest.GitOutput(t, dir, "config", "--type=bool", "--default=false", "commit.gpgsign"); got != "false" {
					t.Errorf("inherited commit signing = %q, want false", got)
				}
				if got := gitAfterConfigIsolation(t, dir, "config", "--type=bool", "--default=false", "commit.gpgsign"); got != "false" {
					t.Errorf("later Git child inherited signing = %q, want false", got)
				}
			})
			if os.Getenv("GIT_CONFIG_PARAMETERS") != parameters {
				t.Error("GitOutput did not restore the caller's parameters at test cleanup")
			}
		})
	}
}

func TestIsolationKeepsTestGlobalConfig(t *testing.T) {
	root := t.TempDir()
	include := filepath.Join(root, "included")
	global := filepath.Join(root, "global")
	if err := os.WriteFile(include, []byte("[user]\nname = Included Person\nemail = included@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(fmt.Sprintf("[include]\npath = %q\n", filepath.ToSlash(include))), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	dir := t.TempDir()
	gittest.RunGit(t, dir, "init")
	if got := gittest.GitOutput(t, dir, "config", "--global", "--includes", "--get", "user.name"); got != "Included Person" {
		t.Errorf("test's included global identity = %q, want Included Person", got)
	}
	if got := gitAfterConfigIsolation(t, dir, "config", "--global", "--includes", "--get", "user.email"); got != "included@example.com" {
		t.Errorf("later Git child global identity = %q, want included@example.com", got)
	}
}

func gitAfterConfigIsolation(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test fixtures only
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("later Git child %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
