package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// maintenanceWorkflow renders the auto-release workflow of a fork line
// consumed from giantswarm whose maintenance branches release too.
func maintenanceWorkflow(t *testing.T, flavour gen.Flavour) string {
	t.Helper()

	w, err := New(Config{Flavours: gen.FlavourSlice{flavour}, ReleaseBranch: "giantswarm", MaintenanceBranches: true})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	return renderInput(t, w.AutoRelease())
}

// maintenanceStep returns the tag-job step with the given id of the
// maintenance-branch fork line's workflow.
func maintenanceStep(t *testing.T, id string) autoReleaseStep {
	t.Helper()

	rendered := maintenanceWorkflow(t, gen.FlavourFork)
	var wf struct {
		Jobs map[string]struct {
			Steps []autoReleaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}
	for _, s := range wf.Jobs["tag"].Steps {
		if s.ID == id {
			return s
		}
	}

	t.Fatalf("no step with id %q in the tag job:\n%s", id, rendered)
	return autoReleaseStep{}
}

// maintenanceFork builds a fork line with a maintenance branch: the line
// releases v1.2.0, release-1.2 branches off it and carries v1.2.1, and the
// consumed branch moves on to v1.3.0. HEAD is release-1.2 with the given
// commits on top.
func maintenanceFork(t *testing.T, commits ...string) string {
	t.Helper()

	dir := repo(t, "feat: line", "v1.2.0")
	mustGit(t, dir, "checkout", "-q", "-b", "release-1.2")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "fix: carried")
	mustGit(t, dir, "tag", "v1.2.1")
	mustGit(t, dir, "checkout", "-q", "main")
	mustGit(t, dir, "commit", "--allow-empty", "-m", "feat: next minor")
	mustGit(t, dir, "tag", "v1.3.0")
	mustGit(t, dir, "checkout", "-q", "release-1.2")
	for _, c := range commits {
		mustGit(t, dir, "commit", "--allow-empty", "-m", c)
	}

	return dir
}

// runMaintenanceStep runs a step of the maintenance-branch workflow in dir on
// the given branch and returns its GITHUB_ENV and GITHUB_OUTPUT.
func runMaintenanceStep(t *testing.T, id, dir, branch string, env ...string) (written, outputs map[string]string, log string, err error) {
	t.Helper()

	envPath := filepath.Join(t.TempDir(), "github-env")
	if err := os.WriteFile(envPath, nil, 0o600); err != nil {
		t.Fatalf("seed GITHUB_ENV: %v", err)
	}

	env = append([]string{"GITHUB_ENV=" + envPath, "GITHUB_REF_NAME=" + branch}, env...)
	outputs, log, err = runStep(t, maintenanceStep(t, id).Run, dir, env...)

	raw, readErr := os.ReadFile(envPath) // #nosec G304 -- path built from t.TempDir
	if readErr != nil {
		t.Fatalf("read GITHUB_ENV: %v", readErr)
	}
	written = map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			written[key] = value
		}
	}

	return written, outputs, log, err
}

// Test_AutoReleaseMaintenanceRender pins the opt-in's reach: a fork line's
// workflow adds its release-X.Y branches, matched whole; another flavour, and
// a fork line without it, render as before.
func Test_AutoReleaseMaintenanceRender(t *testing.T) {
	branches := func(rendered string) []string {
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

	if got := branches(maintenanceWorkflow(t, gen.FlavourFork)); !slices.Equal(got, []string{"giantswarm", "release-[0-9]+.[0-9]+"}) {
		t.Errorf("fork line pushes: got %q, want giantswarm and release-[0-9]+.[0-9]+", got)
	}

	app, err := New(Config{Flavours: gen.FlavourSlice{gen.FlavourApp}, ReleaseBranch: "giantswarm"})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	if got, want := maintenanceWorkflow(t, gen.FlavourApp), renderInput(t, app.AutoRelease()); got != want {
		t.Errorf("an app repository's workflow changes with the fork line's opt-in")
	}

	rendered := maintenanceWorkflow(t, gen.FlavourFork)
	for _, want := range []string{`--bump "${LINE_BUMP:-auto}"`, "LINE_BUMP=patch"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the maintenance-branch workflow lacks %q", want)
		}
	}
	_, plain := tagJobStepsFor(t, gen.FlavourFork)
	if strings.Contains(plain, "LINE_BUMP") {
		t.Errorf("a fork line without the opt-in renders the maintenance branches' patch bump")
	}
}

// Test_AutoReleaseMaintenanceBaseline pins the baseline of each branch: a
// maintenance branch counts from its series' highest stable tag and bumps
// the patch alone, the consumed branch keeps the line's, and a series with
// no stable tag refuses.
func Test_AutoReleaseMaintenanceBaseline(t *testing.T) {
	t.Run("maintenance branch", func(t *testing.T) {
		env, _, log, err := runMaintenanceStep(t, "line", maintenanceFork(t), "release-1.2")
		if err != nil {
			t.Fatalf("line step failed: %v\n%s", err, log)
		}
		if env["LINE_TAG"] != "v1.2.1" {
			t.Errorf("LINE_TAG: got %q, want v1.2.1", env["LINE_TAG"])
		}
		if env["GIT_CLIFF__GIT__TAG_PATTERN"] != `^v1\.2\.[0-9]+$` {
			t.Errorf("GIT_CLIFF__GIT__TAG_PATTERN: got %q", env["GIT_CLIFF__GIT__TAG_PATTERN"])
		}
		if env["LINE_BUMP"] != "patch" {
			t.Errorf("LINE_BUMP: got %q, want patch", env["LINE_BUMP"])
		}
	})

	t.Run("consumed branch", func(t *testing.T) {
		dir := maintenanceFork(t)
		mustGit(t, dir, "checkout", "-q", "main")

		env, _, log, err := runMaintenanceStep(t, "line", dir, "giantswarm")
		if err != nil {
			t.Fatalf("line step failed: %v\n%s", err, log)
		}
		if env["LINE_TAG"] != "v1.3.0" {
			t.Errorf("LINE_TAG: got %q, want v1.3.0", env["LINE_TAG"])
		}
		if env["GIT_CLIFF__GIT__TAG_PATTERN"] != `^v1\.[0-9]+\.[0-9]+$` {
			t.Errorf("GIT_CLIFF__GIT__TAG_PATTERN: got %q", env["GIT_CLIFF__GIT__TAG_PATTERN"])
		}
		if _, set := env["LINE_BUMP"]; set {
			t.Errorf("LINE_BUMP is set on the consumed branch")
		}
	})

	t.Run("series without a stable tag", func(t *testing.T) {
		dir := maintenanceFork(t)
		mustGit(t, dir, "checkout", "-q", "-b", "release-1.4")

		_, _, log, err := runMaintenanceStep(t, "line", dir, "release-1.4")
		if err == nil || !strings.Contains(log, "no stable v1.4.Z tag") {
			t.Errorf("line step on a series without a stable tag: err %v\n%s", err, log)
		}
	})
}

// Test_AutoReleaseMaintenanceCliff pins what the "Compute next version" step
// computes on a maintenance branch: the next patch of the series even for a
// feature, never the consumed branch's next minor, and nothing for a push
// git-cliff counts no commit of.
func Test_AutoReleaseMaintenanceCliff(t *testing.T) {
	requireGitCliff(t)

	for _, tc := range []struct {
		name   string
		commit string
		want   string
	}{
		{name: "feature", commit: "feat: backported", want: "v1.2.2"},
		{name: "fix", commit: "fix: backported", want: "v1.2.2"},
		{name: "docs only", commit: "docs: a typo", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := maintenanceFork(t, tc.commit)
			cliffTomlIn(t, dir)

			env, _, log, err := runMaintenanceStep(t, "line", dir, "release-1.2")
			if err != nil {
				t.Fatalf("line step failed: %v\n%s", err, log)
			}
			vars := make([]string, 0, len(env))
			for k, v := range env {
				vars = append(vars, k+"="+v)
			}

			_, out, log, err := runMaintenanceStep(t, "cliff", dir, "release-1.2", vars...)
			if err != nil {
				t.Fatalf("cliff step failed: %v\n%s", err, log)
			}
			if out["version"] != tc.want {
				t.Errorf("version: got %q, want %q\n%s", out["version"], tc.want, log)
			}
		})
	}
}
