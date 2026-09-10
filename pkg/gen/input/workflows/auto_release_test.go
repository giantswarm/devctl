package workflows

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// decideScript extracts the shell of the auto-release "Decide whether to tag"
// step from the rendered workflow, so the tests below exercise the script that
// actually ships rather than a copy of it.
func decideScript(t *testing.T) string {
	t.Helper()

	rendered := renderInput(t, newWorkflows(t, gen.FlavourApp).AutoRelease())

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string `yaml:"id"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}

	for _, step := range wf.Jobs["tag"].Steps {
		if step.ID == "decide" {
			return step.Run
		}
	}

	t.Fatalf("no step with id \"decide\" in the tag job:\n%s", rendered)
	return ""
}

// repo builds a git history for one test case. A "vX.Y.Z" entry tags the
// current HEAD, anything else becomes an empty commit with that subject.
func repo(t *testing.T, history ...string) string {
	t.Helper()

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "--initial-branch=main")
	run("commit", "--allow-empty", "-m", "chore: inception")
	for _, entry := range history {
		if strings.HasPrefix(entry, "v") {
			run("tag", entry)
			continue
		}
		run("commit", "--allow-empty", "-m", entry)
	}

	return dir
}

// decide runs the extracted step in dir and returns its GITHUB_OUTPUT as a map.
func decide(t *testing.T, script, dir, next, want string) map[string]string {
	t.Helper()

	outPath := filepath.Join(t.TempDir(), "github-output")
	if err := os.WriteFile(outPath, nil, 0o600); err != nil {
		t.Fatalf("seed GITHUB_OUTPUT: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"NEXT="+next,
		"WANT="+want,
		"GITHUB_REF_NAME=main",
		"GITHUB_OUTPUT="+outPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("decide step failed: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(outPath) // #nosec G304 -- path built from t.TempDir
	if err != nil {
		t.Fatalf("read GITHUB_OUTPUT: %v", err)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}

	return got
}

// Test_AutoReleaseDecide pins the release-candidate rule: tag an RC when at
// least one unreleased commit is feat-rc/fix-rc and no unreleased feat, fix or
// breaking commit lacks the -rc. `next` is what git-cliff computes, which is
// always the stable target because cliff.toml normalises the -rc away.
func Test_AutoReleaseDecide(t *testing.T) {
	script := decideScript(t)

	testCases := []struct {
		name           string
		history        []string
		next           string
		want           string
		expectTag      string
		expectPrerelse string
	}{
		{
			name:           "case 0: no marker anywhere is a stable release",
			history:        []string{"v1.2.9", "feat: add x"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0",
			expectPrerelse: "false",
		},
		{
			name:           "case 1: a single feat-rc opens the cycle",
			history:        []string{"v1.2.9", "feat-rc: add x"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "case 2: chore(deps) does not decide, so the cycle holds",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "chore(deps): bump y"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			name:           "case 3: docs does not decide either",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "docs: fix typo"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			name:           "case 4: an unmarked fix closes the cycle at the stable target",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "fix: last thing"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0",
			expectPrerelse: "false",
		},
		{
			name:           "case 5: an unmarked breaking change of any type closes the cycle",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "refactor!: drop y"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0",
			expectPrerelse: "false",
		},
		{
			name:           "a marked breaking change gives a major RC",
			history:        []string{"v1.2.9", "feat-rc!: drop v1"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "scopes are accepted on both sides of the rule",
			history:        []string{"v1.2.9", "feat-rc(auth): add x", "v1.3.0-rc.1", "chore(deps): bump y"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			name:           "rc.9 is followed by rc.10, not rc.2",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.9", "fix-rc: tweak"},
			next:           "v1.3.0",
			expectTag:      "v1.3.0-rc.10",
			expectPrerelse: "true",
		},
		{
			name:           "a moved target restarts the RC series at rc.1",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1", "feat-rc!: drop v1"},
			next:           "v2.0.0",
			expectTag:      "v2.0.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:           "inception, with no tag at all",
			history:        []string{"feat-rc: add x"},
			next:           "v0.1.0",
			expectTag:      "v0.1.0-rc.1",
			expectPrerelse: "true",
		},
		{
			name:      "no releasable commits, so no tag",
			history:   []string{"v1.3.0"},
			next:      "v1.3.0",
			expectTag: "",
		},
		{
			// The stable target equals the last stable tag, but the RC series
			// is not finished, so an explicit `rc` must still produce one.
			name:           "workflow_dispatch rc forces one more candidate",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1"},
			next:           "v1.3.0",
			want:           "rc",
			expectTag:      "v1.3.0-rc.2",
			expectPrerelse: "true",
		},
		{
			name:           "workflow_dispatch stable closes a cycle with nothing left to merge",
			history:        []string{"v1.2.9", "feat-rc: add x", "v1.3.0-rc.1"},
			next:           "v1.3.0",
			want:           "stable",
			expectTag:      "v1.3.0",
			expectPrerelse: "false",
		},
		{
			// On release-2.x the reachable baseline is v2.3.5, not the higher
			// v3.x tags on main, which git-cliff has already accounted for in
			// `next`. The step must agree, or it reports "nothing to release".
			name:           "a backport branch bumps from its own reachable baseline",
			history:        []string{"v2.3.5", "fix: backport"},
			next:           "v2.3.6",
			expectTag:      "v2.3.6",
			expectPrerelse: "false",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want
			if want == "" {
				want = "auto"
			}

			got := decide(t, script, repo(t, tc.history...), tc.next, want)

			if got["tag"] != tc.expectTag {
				t.Errorf("tag = %q, want %q", got["tag"], tc.expectTag)
			}
			if got["prerelease"] != tc.expectPrerelse {
				t.Errorf("prerelease = %q, want %q", got["prerelease"], tc.expectPrerelse)
			}
		})
	}
}

// Test_AutoReleaseDescribeExcludesPreReleases pins the --exclude flag. Without
// it `git describe --match='v*.*.*'` returns a reachable v1.3.0-rc.N as the
// baseline, the "nothing to release" comparison can never be true again, and
// the job tags on every push.
func Test_AutoReleaseDescribeExcludesPreReleases(t *testing.T) {
	script := decideScript(t)

	if !strings.Contains(script, "--exclude='*-*'") {
		t.Errorf("decide step must exclude pre-release tags from the describe baseline:\n%s", script)
	}
}

// Test_AutoReleaseCliffNormalisesRcTypes pins the contract between the
// workflow and cliff.toml: the workflow reads the raw -rc subjects, so
// git-cliff must be handed the plain type. features_always_bump_minor keys off
// the type being exactly "feat", and an un-normalised feat-rc bumps patch.
func Test_AutoReleaseCliffNormalisesRcTypes(t *testing.T) {
	cliff := renderInput(t, newWorkflows(t, gen.FlavourApp).CliffToml())

	if !strings.Contains(cliff, `{ pattern = '^(feat|fix)-rc', replace = "${1}" }`) {
		t.Errorf("cliff.toml does not normalise feat-rc/fix-rc before parsing:\n%s", cliff)
	}
	if !strings.Contains(cliff, "features_always_bump_minor = true") {
		t.Error("cliff.toml no longer sets features_always_bump_minor; the normalisation above may be pointless")
	}
}

// Test_SemanticPullRequestAcceptsRcTypes pins the other half of that contract.
// The action's stock parser reads the type with `\w*`, so the widened
// headerPattern is what lets a hyphenated type through at all; `types` alone
// would not.
func Test_SemanticPullRequestAcceptsRcTypes(t *testing.T) {
	got := renderInput(t, newWorkflows(t, gen.FlavourApp).SemanticPullRequest())

	var wf struct {
		Jobs map[string]struct {
			With map[string]string `yaml:"with"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(got), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, got)
	}

	with := wf.Jobs["semantic-pull-request"].With

	if with["headerPattern"] != `^(\w*(?:-rc)?)(?:\((.*)\))?!?: (.*)$` {
		t.Errorf("headerPattern = %q, want the type group widened by exactly the -rc suffix", with["headerPattern"])
	}

	// `types` replaces the action's default list rather than extending it, so
	// dropping one of these silently blocks that type on every repository.
	for _, want := range []string{
		"feat(-rc)?", "fix(-rc)?", "docs", "style", "refactor",
		"perf", "test", "build", "ci", "chore", "revert",
	} {
		if !strings.Contains(with["types"], want+"\n") {
			t.Errorf("types is missing %q:\n%s", want, with["types"])
		}
	}
}
