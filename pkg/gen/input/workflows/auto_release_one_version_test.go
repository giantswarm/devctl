package workflows

import (
	"slices"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Test_AutoReleaseOneVersionOrder pins where the one-version check runs: on
// every run that tags, after the decide step named the tag and its commit
// and before the step that creates the tag and the release, so a refusal
// cuts neither. Every flavour carries it.
func Test_AutoReleaseOneVersionOrder(t *testing.T) {
	for _, flavour := range []gen.Flavour{gen.FlavourApp, gen.FlavourFork} {
		t.Run(string(flavour), func(t *testing.T) {
			steps, rendered := tagJobStepsFor(t, flavour)
			ids := make([]string, 0, len(steps))
			for _, s := range steps {
				ids = append(ids, s.ID)
			}

			decide, check, release := slices.Index(ids, "decide"), slices.Index(ids, "one-version"), slices.Index(ids, "release")
			if check < 0 {
				t.Fatalf("no step with id \"one-version\" in the tag job:\n%s", rendered)
			}
			if check < decide || check > release {
				t.Errorf("step order %v: the one-version check has to run after decide and before release", ids)
			}
			if want := "steps.decide.outputs.tag != ''"; steps[check].If != want {
				t.Errorf("one-version if = %q, want %q", steps[check].If, want)
			}
		})
	}
}

// Test_AutoReleaseOneVersion pins what the check refuses: a tag on a commit
// that already carries another version's release tag, lightweight or
// annotated, named in the error. A candidate's stable on the candidate's
// commit, an upstream pre-release, a name that is no version and a tag on
// another commit pass, as does a commit without a tag.
func Test_AutoReleaseOneVersion(t *testing.T) {
	check := tagJobStep(t, "one-version").Run

	testCases := []struct {
		name      string
		tags      []string
		annotated []string
		tag       string
		expectErr bool
		expectLog string
	}{
		{
			name:      "an untagged commit",
			tag:       "v1.3.0-rc.1",
			expectLog: "v1.3.0-rc.1 is its only version",
		},
		{
			name:      "a stable on its candidate's commit",
			tags:      []string{"v1.3.0-rc.2"},
			tag:       "v1.3.0",
			expectLog: "v1.3.0 is its only version",
		},
		{
			name:      "an upstream pre-release and a name that is no version",
			tags:      []string{"v1.4.0-alpha.1", "latest"},
			tag:       "v1.3.0-rc.1",
			expectLog: "v1.3.0-rc.1 is its only version",
		},
		{
			name:      "another version's stable",
			tags:      []string{"v1.1.4"},
			tag:       "v1.2.0",
			expectErr: true,
			expectLog: "::error title=Another version on the commit::v1.2.0 would land on",
		},
		{
			name:      "another version's candidate",
			tags:      []string{"v1.3.0-rc.1"},
			tag:       "v1.2.1-rc.1",
			expectErr: true,
			expectLog: "already carries the release tag of another version: v1.3.0-rc.1.",
		},
		{
			name:      "another version's annotated tag",
			annotated: []string{"v1.1.4"},
			tag:       "v1.2.0",
			expectErr: true,
			expectLog: "already carries the release tag of another version: v1.1.4.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// The checkout's origin: the history, an older release on the
			// commit before, and the commit about to be tagged.
			origin := repo(t, "feat: one", "v1.0.0", "feat: two")
			for _, tag := range tc.tags {
				mustGit(t, origin, "tag", tag)
			}
			for _, tag := range tc.annotated {
				mustGit(t, origin, "-c", "user.name=t", "-c", "user.email=t@example.com", "tag", "-a", "-m", tag, tag)
			}
			target := strings.TrimSpace(mustGit(t, origin, "rev-parse", "HEAD"))

			// The checkout knows none of the tags: they are read from origin.
			dir := t.TempDir()
			mustGit(t, dir, "init", "--initial-branch=main")
			mustGit(t, dir, "remote", "add", "origin", origin)

			_, log, err := runStep(t, check, dir, "TAG="+tc.tag, "TARGET="+target)
			if tc.expectErr != (err != nil) {
				t.Fatalf("err = %v, want error %v\n%s", err, tc.expectErr, log)
			}
			if !strings.Contains(log, tc.expectLog) {
				t.Errorf("log does not contain %q:\n%s", tc.expectLog, log)
			}
			if tc.expectErr && !strings.Contains(log, tc.tag) {
				t.Errorf("log does not name %s:\n%s", tc.tag, log)
			}
		})
	}
}
