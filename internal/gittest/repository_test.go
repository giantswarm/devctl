package gittest_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/gittest"
)

// independentGit creates and inspects only synthetic repositories without
// depending on the environment isolation being tested.
func independentGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test fixtures only
	cmd.Env = []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("independent git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestInheritedGitRepositoryEnvironment(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)

	t.Run("GitOutput uses the requested repository", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		independentGit(t, dir, "init", "--template=", "-b", "main")
		independentGit(t, outside, "init", "--template=", "-b", "main")
		t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
		t.Setenv("GIT_WORK_TREE", outside)
		got := gittest.GitOutput(t, dir, "rev-parse", "--absolute-git-dir")
		want := independentGit(t, dir, "rev-parse", "--absolute-git-dir")
		if got != want {
			t.Errorf("GitOutput opened %q, want %q", got, want)
		}
		if got, want := gittest.GitOutput(t, dir, "rev-parse", "--show-toplevel"), independentGit(t, dir, "rev-parse", "--show-toplevel"); got != want {
			t.Errorf("GitOutput worktree = %q, want %q", got, want)
		}
		if got := gitAfterRepositoryIsolation(t, dir, "rev-parse", "--absolute-git-dir"); got != want {
			t.Errorf("later Git child opened %q, want %q", got, want)
		}
	})

	t.Run("RunGit initializes only the requested repository", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		independentGit(t, outside, "init", "--template=", "-b", "main")
		independentGit(t, outside, "config", "user.name", "Outside Person")
		config := filepath.Join(outside, ".git", "config")
		before, err := os.ReadFile(config) // #nosec G304 -- test fixture
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_DIR", filepath.Join(outside, ".git"))
		t.Setenv("GIT_WORK_TREE", outside)
		gittest.RunGit(t, dir, "init", "--template=")
		after, err := os.ReadFile(config) // #nosec G304 -- test fixture
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Error("fixture initialization changed the outside repository's config")
		}
		if got := independentGit(t, dir, "config", "--local", "--get", "user.name"); got != "test" {
			t.Errorf("fixture local identity = %q, want test", got)
		}
	})

	t.Run("RunGit preserves an outside index", func(t *testing.T) {
		dir, outside := t.TempDir(), t.TempDir()
		independentGit(t, dir, "init", "--template=", "-b", "main")
		independentGit(t, outside, "init", "--template=", "-b", "main")
		for _, repo := range []string{dir, outside} {
			if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte(repo), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		independentGit(t, outside, "add", "file.txt")
		index := filepath.Join(outside, ".git", "index")
		before, err := os.ReadFile(index) // #nosec G304 -- test fixture
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_INDEX_FILE", index)
		gittest.RunGit(t, dir, "add", "file.txt")
		after, err := os.ReadFile(index) // #nosec G304 -- test fixture
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Error("RunGit changed the outside index")
		}
		if got := independentGit(t, dir, "ls-files"); got != "file.txt" {
			t.Errorf("fixture index contains %q, want file.txt", got)
		}
		if got := gitAfterRepositoryIsolation(t, dir, "ls-files"); got != "file.txt" {
			t.Errorf("later Git child's index contains %q, want file.txt", got)
		}
	})

	t.Run("RunGit keeps objects in the fixture", func(t *testing.T) {
		dir, objects := t.TempDir(), t.TempDir()
		independentGit(t, dir, "init", "--template=", "-b", "main")
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("fixture contents\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_OBJECT_DIRECTORY", objects)
		gittest.RunGit(t, dir, "add", "file.txt")
		entries, err := os.ReadDir(objects)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Error("RunGit wrote into the outside object directory")
		}
		hash := independentGit(t, dir, "hash-object", "file.txt")
		if got := independentGit(t, dir, "cat-file", "-p", hash); got != "fixture contents" {
			t.Errorf("fixture's stored object = %q, want fixture contents", got)
		}
	})

	t.Run("RunGit preserves an outside config", func(t *testing.T) {
		dir := t.TempDir()
		config := filepath.Join(t.TempDir(), "config")
		before := []byte("[user]\nname = Outside Person\n")
		if err := os.WriteFile(config, before, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG", config)
		gittest.RunGit(t, dir, "init", "--template=")
		after, err := os.ReadFile(config) // #nosec G304 -- test fixture
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Error("RunGit wrote fixture identity into the outside config")
		}
		if got := independentGit(t, dir, "config", "--local", "--get", "user.name"); got != "test" {
			t.Errorf("fixture local identity = %q, want test", got)
		}
	})
}

func TestRepositoryEnvironmentRestored(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for name, original := range map[string]string{"path": filepath.Join(t.TempDir(), ".git"), "empty": ""} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GIT_DIR", original)
			t.Run("fixture", func(t *testing.T) {
				gittest.RunGit(t, t.TempDir(), "init", "--template=")
			})
			if got, present := os.LookupEnv("GIT_DIR"); !present || got != original {
				t.Error("test cleanup did not restore the original repository environment")
			}
		})
	}
}

func gitAfterRepositoryIsolation(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test fixtures only
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("later Git child %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
