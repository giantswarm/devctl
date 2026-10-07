package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// forkLineStep returns the fork line's baseline step, which only a fork
// line's workflow declares.
func forkLineStep(t *testing.T) autoReleaseStep {
	t.Helper()

	steps, rendered := tagJobStepsFor(t, gen.FlavourFork)
	for _, s := range steps {
		if s.ID == "line" {
			return s
		}
	}

	t.Fatalf("no step with id \"line\" in the fork line's tag job:\n%s", rendered)
	return autoReleaseStep{}
}

// runLineStep runs the fork line's baseline step in dir and returns what it
// wrote to GITHUB_ENV as a map.
func runLineStep(t *testing.T, dir string) map[string]string {
	t.Helper()

	envPath := filepath.Join(t.TempDir(), "github-env")
	if err := os.WriteFile(envPath, nil, 0o600); err != nil {
		t.Fatalf("seed GITHUB_ENV: %v", err)
	}

	_, out, err := runStep(t, forkLineStep(t).Run, dir, "GITHUB_ENV="+envPath, "GITHUB_REF_NAME=giantswarm")
	if err != nil {
		t.Fatalf("line step failed: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(envPath) // #nosec G304 -- path built from t.TempDir
	if err != nil {
		t.Fatalf("read GITHUB_ENV: %v", err)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}

	return got
}

// repinnedFork builds a fork line that was re-pinned: upstream tags v0.5.0,
// the line branches off it and releases v2.1.0 and v2.1.1, upstream moves on
// to v0.6.0, and the line is rebased onto it, which leaves v2.1.x behind.
func repinnedFork(t *testing.T) string {
	t.Helper()

	dir := repo(t, "feat: upstream a", "v0.5.0")
	mustGit(t, dir, "checkout", "-q", "-b", "giantswarm")
	for _, entry := range []string{"feat(fork): carried patch", "v2.1.0", "fix(fork): another patch", "v2.1.1"} {
		if strings.HasPrefix(entry, "v") {
			mustGit(t, dir, "tag", entry)
			continue
		}
		mustGit(t, dir, "commit", "--allow-empty", "-m", entry)
	}
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "feat: upstream b")
	mustGit(t, dir, "tag", "v0.6.0")
	mustGit(t, dir, "rebase", "-q", "--onto", "v0.6.0", "v0.5.0", "giantswarm")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "fix: after the re-pin")

	return dir
}

// Test_AutoReleaseForkRender pins what a fork line's workflow triggers on: its
// consumed branch alone, no backport branch, and only its workflow declares
// the baseline step.
func Test_AutoReleaseForkRender(t *testing.T) {
	branches := func(flavour gen.Flavour, releaseBranch string) []string {
		w, err := New(Config{Flavours: gen.FlavourSlice{flavour}, ReleaseBranch: releaseBranch})
		if err != nil {
			t.Fatalf("New() returned unexpected error: %v", err)
		}
		rendered := renderInput(t, w.AutoRelease())

		var wf struct {
			On struct {
				Push struct {
					Branches []string `yaml:"branches"`
				} `yaml:"push"`
			} `yaml:"on"`
		}
		if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
			t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
		}
		return wf.On.Push.Branches
	}

	if got := branches(gen.FlavourFork, "giantswarm"); !slices.Equal(got, []string{"giantswarm"}) {
		t.Errorf("fork line pushes: got %q, want only giantswarm", got)
	}
	if got := branches(gen.FlavourApp, ""); len(got) < 2 || got[0] != "main" {
		t.Errorf("app pushes: got %q, want main and the backport branches", got)
	}

	steps, _ := tagJobSteps(t)
	for _, s := range steps {
		if s.ID == "line" {
			t.Errorf("a repository that is no fork line declares the fork line's baseline step")
		}
	}
}

// Test_AutoReleaseForkLineBaseline pins the fork line's baseline: its highest
// stable tag, whether the branch still reaches it or a re-pin left it behind,
// never a mirrored upstream tag, and the higher of two stable tags on one
// commit.
func Test_AutoReleaseForkLineBaseline(t *testing.T) {
	t.Run("re-pinned", func(t *testing.T) {
		dir := repinnedFork(t)

		env := runLineStep(t, dir)
		if env["LINE_TAG"] != "v2.1.1" {
			t.Errorf("LINE_TAG: got %q, want v2.1.1", env["LINE_TAG"])
		}
		if env["GIT_CLIFF__GIT__TAG_PATTERN"] != `^v2\.[0-9]+\.[0-9]+$` {
			t.Errorf("GIT_CLIFF__GIT__TAG_PATTERN: got %q", env["GIT_CLIFF__GIT__TAG_PATTERN"])
		}
		if _, err := gitIn(t, dir, "merge-base", "--is-ancestor", "v2.1.1", "HEAD"); err != nil {
			t.Errorf("v2.1.1 is not moved onto the branch: %v", err)
		}
		if env["LINE_REPINNED"] != "true" {
			t.Errorf("LINE_REPINNED: got %q, want true", env["LINE_REPINNED"])
		}
	})

	t.Run("two stable tags on one commit", func(t *testing.T) {
		dir := repo(t, "feat: a", "v1.1.3", "fix: b", "v1.2.0", "v1.1.4")

		env := runLineStep(t, dir)
		if got := env["LINE_TAG"]; got != "v1.2.0" {
			t.Errorf("LINE_TAG: got %q, want v1.2.0", got)
		}
		if _, set := env["LINE_REPINNED"]; set {
			t.Errorf("LINE_REPINNED is set on a branch that still holds its stable tag")
		}
	})

	t.Run("no stable tag yet", func(t *testing.T) {
		dir := repo(t, "feat: a", "v0.1.0-rc.1")

		if got := runLineStep(t, dir); len(got) != 0 {
			t.Errorf("GITHUB_ENV: got %v, want nothing", got)
		}
	})
}

// Test_AutoReleaseForkCliffBumpsFromTheLine pins what git-cliff computes after
// a re-pin with the baseline step's environment: the next minor of the line,
// never the next version of upstream's series.
func Test_AutoReleaseForkCliffBumpsFromTheLine(t *testing.T) {
	requireGitCliff(t)

	dir := repinnedFork(t)
	env := runLineStep(t, dir)
	cliffTomlIn(t, dir)

	cmd := exec.CommandContext(t.Context(), "git-cliff", "--unreleased", "--bump", "--context")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CLIFF__GIT__TAG_PATTERN="+env["GIT_CLIFF__GIT__TAG_PATTERN"])
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git-cliff: %v\n%s", err, out)
	}

	if !strings.Contains(string(out), `"version":"v2.2.0"`) {
		t.Errorf("git-cliff did not bump the line to v2.2.0:\n%s", out)
	}
}
