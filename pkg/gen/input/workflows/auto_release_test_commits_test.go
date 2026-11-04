package workflows

import (
	"strings"
	"testing"
)

// Test_AutoReleaseTestCommits runs the rendered compute and decide steps with
// real git-cliff and local histories. A test fix can replace a red candidate,
// but cannot open a release cycle or keep spending candidate numbers on reruns.
func Test_AutoReleaseTestCommits(t *testing.T) {
	requireGitCliff(t)
	compute := tagJobStep(t, "cliff").Run

	for _, tc := range []struct {
		name      string
		history   []string
		patch     bool
		otherRC   bool
		expectTag string
	}{
		{
			name:      "case 0: a test fix advances a minor candidate",
			history:   []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "test: fix the failing test"},
			expectTag: "v1.3.0-rc.2",
		},
		{
			name:      "case 1: a scoped test fix advances a major candidate",
			history:   []string{"v1.2.9", "feat!: replace x", "v2.0.0-rc.9", "test(unit): fix the failing test"},
			expectTag: "v2.0.0-rc.10",
		},
		{
			name:      "case 2: a test fix advances an opted-in patch candidate",
			history:   []string{"v1.2.9", "fix: x", "v1.2.10-rc.1", "test: fix the failing test"},
			patch:     true,
			expectTag: "v1.2.10-rc.2",
		},
		{
			name:      "case 3: the first cycle needs no stable baseline",
			history:   []string{"feat: add x", "v0.1.0-rc.1", "test: fix the failing test"},
			expectTag: "v0.1.0-rc.2",
		},
		{
			name:    "case 4: tests alone after a stable release cut nothing",
			history: []string{"v1.2.9", "test: fix the failing test"},
		},
		{
			name:    "case 5: tests after a promoted candidate cut nothing",
			history: []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "v1.3.0", "test: fix the failing test"},
		},
		{
			name:    "case 6: docs and style still cut nothing during a cycle",
			history: []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "docs: a typo", "style: formatting"},
		},
		{
			name:    "case 7: tests already in the latest candidate cut nothing",
			history: []string{"v1.2.9", "feat: add x", "test: fix the failing test", "v1.3.0-rc.2"},
		},
		{
			name:    "case 8: earlier tests do not make a docs-only push release",
			history: []string{"v1.2.9", "feat: add x", "test: fix the failing test", "v1.3.0-rc.2", "docs: a typo"},
		},
		{
			name:    "case 9: a test subject in a docs body does not count",
			history: []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "docs: examples\n\ntest: an example subject"},
		},
		{
			name:    "case 10: a type starting with test does not count",
			history: []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1", "testing: an internal fixture"},
		},
		{
			name:    "case 11: a candidate on another branch opens no cycle",
			history: []string{"v1.2.9", "test: fix the failing test"},
			otherRC: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := repo(t, tc.history...)
			if tc.otherRC {
				mustGit(t, dir, "checkout", "-b", "other", "v1.2.9")
				mustGit(t, dir, "commit", "--allow-empty", "-m", "feat: another branch")
				mustGit(t, dir, "tag", "v1.3.0-rc.1")
				mustGit(t, dir, "checkout", "main")
			}
			cliffTomlIn(t, dir) // Drop the remote section: no GitHub lookups.
			computed, log, err := runStep(t, compute, dir)
			if err != nil {
				t.Fatalf("compute step failed: %v\n%s", err, log)
			}
			script := decideScript(t)
			if tc.patch {
				script = patchCandidatesDecideScript(t)
			}
			got, log, err := runStep(t, script, dir,
				"NEXT="+computed["version"], "WANT=auto", "CLIFF_CONTEXT=cliff-context.json")
			if err != nil {
				t.Fatalf("decide step failed: %v\n%s", err, log)
			}
			if got["tag"] != tc.expectTag {
				t.Fatalf("tag = %q, want %q\n%s", got["tag"], tc.expectTag, log)
			}
			if tc.expectTag == "" {
				return
			}
			if got["prerelease"] != "true" {
				t.Errorf("prerelease = %q, want true", got["prerelease"])
			}
			if head := strings.TrimSpace(mustGit(t, dir, "rev-parse", "HEAD")); got["target"] != head {
				t.Errorf("target = %q, want HEAD %q", got["target"], head)
			}
			// A queued rerun on the newly tagged commit must do nothing.
			mustGit(t, dir, "tag", tc.expectTag)
			got, log, err = runStep(t, script, dir,
				"NEXT="+computed["version"], "WANT=auto", "CLIFF_CONTEXT=cliff-context.json")
			if err != nil || got["tag"] != "" {
				t.Errorf("rerun: tag = %q, err = %v, want no tag\n%s", got["tag"], err, log)
			}
		})
	}
}
