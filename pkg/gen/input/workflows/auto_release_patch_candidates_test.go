package workflows

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// patchCandidatesWorkflow renders the auto-release workflow of a repository
// of the given flavour whose patches are cut as candidates too.
func patchCandidatesWorkflow(t *testing.T, flavour gen.Flavour) string {
	t.Helper()

	w, err := New(Config{Flavours: gen.FlavourSlice{flavour}, PatchCandidates: true})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	return renderInput(t, w.AutoRelease())
}

// patchCandidatesDecideScript extracts the shell of the "Decide whether to
// tag" step of that workflow.
func patchCandidatesDecideScript(t *testing.T) string {
	t.Helper()

	rendered := patchCandidatesWorkflow(t, gen.FlavourApp)
	var wf struct {
		Jobs map[string]struct {
			Steps []autoReleaseStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(rendered), &wf); err != nil {
		t.Fatalf("rendered workflow is not valid YAML: %v\n%s", err, rendered)
	}
	for _, s := range wf.Jobs["tag"].Steps {
		if s.ID == "decide" {
			return s.Run
		}
	}

	t.Fatalf("no decide step in the tag job:\n%s", rendered)
	return ""
}

// Test_AutoReleasePatchCandidatesDecide pins the opt-in's rule: a patch of
// the last stable release is a candidate like any other bump, numbered on
// from the patch's last candidate, so no push tags a stable release; the
// rules that cut nothing are unchanged.
func Test_AutoReleasePatchCandidatesDecide(t *testing.T) {
	script := patchCandidatesDecideScript(t)

	for _, tc := range []struct {
		name      string
		history   []string
		next      string
		expectTag string
	}{
		{
			name:      "a fix after a stable release cuts the patch's first candidate",
			history:   []string{"v1.16.4", "fix: x"},
			next:      "v1.16.5",
			expectTag: "v1.16.5-rc.1",
		},
		{
			name:      "a chore(deps) alone cuts a patch candidate too",
			history:   []string{"v1.17.1", "chore(deps): bump y"},
			next:      "v1.17.2",
			expectTag: "v1.17.2-rc.1",
		},
		{
			name:      "the next fix cuts the patch's next candidate",
			history:   []string{"v1.16.4", "fix: x", "v1.16.5-rc.1", "fix: y"},
			next:      "v1.16.5",
			expectTag: "v1.16.5-rc.2",
		},
		{
			name:      "a docs-only push after a patch candidate cuts nothing",
			history:   []string{"v1.16.4", "fix: x", "v1.16.5-rc.1", "docs: a typo"},
			next:      "v1.16.5",
			expectTag: "",
		},
		{
			name:      "a feat still opens a minor cycle",
			history:   []string{"v1.16.4", "feat: add x"},
			next:      "v1.17.0",
			expectTag: "v1.17.0-rc.1",
		},
		{
			name:      "no releasable commits, so no tag",
			history:   []string{"v1.16.4"},
			next:      "v1.16.4",
			expectTag: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, tc.history...)
			got := decide(t, script, dir, tc.next, "auto")

			if got["tag"] != tc.expectTag {
				t.Errorf("tag = %q, want %q", got["tag"], tc.expectTag)
			}
			if tc.expectTag != "" && got["prerelease"] != "true" {
				t.Errorf("prerelease = %q, want true", got["prerelease"])
			}
		})
	}

	t.Run("a stable run promotes the patch candidate", func(t *testing.T) {
		dir := repo(t, "v1.16.4", "fix: x", "v1.16.5-rc.1")
		cliffContext(t, dir)

		got, log, err := runStep(t, script, dir, "NEXT=v1.16.5", "WANT=stable", "PROMOTE=v1.16.5-rc.1", "CLIFF_CONTEXT=cliff-context.json")
		if err != nil {
			t.Fatalf("decide step failed: %v\n%s", err, log)
		}
		if got["tag"] != "v1.16.5" || got["prerelease"] != "false" {
			t.Errorf("tag = %q, prerelease = %q, want v1.16.5 stable", got["tag"], got["prerelease"])
		}
	})
}

// Test_AutoReleasePatchCandidatesRender pins the opt-in's reach: the patch
// fast path is gone from the workflow and its header says so, and a
// repository without it renders the fast path as before.
func Test_AutoReleasePatchCandidatesRender(t *testing.T) {
	const fastPath = "tagged stable, no candidate"

	for _, flavour := range []gen.Flavour{gen.FlavourApp, gen.FlavourFork} {
		rendered := patchCandidatesWorkflow(t, flavour)
		if strings.Contains(rendered, fastPath) {
			t.Errorf("%s: the patch-candidates workflow still tags a patch stable on its push", flavour)
		}
		if !strings.Contains(rendered, "every bump, a patch\n#      included, cuts the next vX.Y.Z-rc.N release candidate") {
			t.Errorf("%s: the header does not say a patch is a candidate", flavour)
		}
	}

	_, plain := tagJobStepsFor(t, gen.FlavourApp)
	if !strings.Contains(plain, fastPath) {
		t.Errorf("a repository without the opt-in lost the patch fast path")
	}
}
