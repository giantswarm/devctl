package workflows

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Exercise the rendered config and both workflow steps together. A mocked
// git-cliff context would hide a parser that accidentally counts these types.
func Test_AutoReleaseNonReleasingCommits(t *testing.T) {
	requireGitCliff(t)

	for _, tc := range []struct {
		name           string
		flavour        gen.Flavour
		history        []string
		following      string
		expectTag      string
		expectSubjects []string
	}{
		{
			name:    "case 0: no first release for skipped commits",
			flavour: gen.FlavourApp,
		},
		{
			name:    "case 1: no patch after a stable release",
			flavour: gen.FlavourApp,
			history: []string{"v1.2.9"},
		},
		{
			name:           "case 2: no next candidate during a minor cycle",
			flavour:        gen.FlavourApp,
			history:        []string{"v1.2.9", "feat: add x", "v1.3.0-rc.1"},
			expectSubjects: []string{"add x"},
		},
		{
			name:           "case 3: no next candidate on a fork line",
			flavour:        gen.FlavourFork,
			history:        []string{"v1.2.9", "feat(fork): add x", "v1.3.0-rc.1"},
			expectSubjects: []string{"add x"},
		},
		{
			name:           "case 4: a later fix still releases without skipped notes",
			flavour:        gen.FlavourApp,
			history:        []string{"v1.2.9"},
			following:      "fix: ship the fix",
			expectTag:      "v1.2.10",
			expectSubjects: []string{"ship the fix"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps, _ := tagJobStepsFor(t, tc.flavour)
			var compute, decide string
			for _, step := range steps {
				switch step.ID {
				case "cliff":
					compute = step.Run
				case "decide":
					decide = step.Run
				}
			}
			if compute == "" || decide == "" {
				t.Fatal("rendered workflow is missing compute or decide step")
			}

			for _, message := range []string{
				"ci: pin the build tool",
				"ci(deps): pin the build tool",
				"chore: align files according to platform standards",
				"chore(deps): bump a dependency",
				"docs: fix a typo",
				"docs(readme): fix a typo",
			} {
				t.Run(message, func(t *testing.T) {
					dir := repo(t, tc.history...)
					if tc.flavour == gen.FlavourFork {
						mustGit(t, dir, "branch", "-m", "giantswarm")
					}
					mustGit(t, dir, "commit", "--allow-empty", "-m", message)
					if tc.following != "" {
						mustGit(t, dir, "commit", "--allow-empty", "-m", tc.following)
					}
					cliffTomlIn(t, dir)

					computed, log, err := runStep(t, compute, dir)
					if err != nil {
						t.Fatalf("compute step: %v\n%s", err, log)
					}
					raw, err := os.ReadFile(filepath.Join(dir, "cliff-context.json")) // #nosec G304 -- temporary fixture
					if err != nil {
						t.Fatal(err)
					}
					var releases []cliffRelease
					if err := json.Unmarshal(raw, &releases); err != nil {
						t.Fatal(err)
					}
					var subjects []string
					for _, release := range releases {
						subjects = append(subjects, release.subjects()...)
					}
					if !slices.Equal(subjects, tc.expectSubjects) {
						t.Errorf("release notes contain %q, want %q", subjects, tc.expectSubjects)
					}

					got, log, err := runStep(t, decide, dir,
						"NEXT="+computed["version"], "WANT=auto", "CLIFF_CONTEXT=cliff-context.json")
					if err != nil {
						t.Fatalf("decide step: %v\n%s", err, log)
					}
					if got["tag"] != tc.expectTag {
						t.Errorf("tag = %q, want %q\n%s", got["tag"], tc.expectTag, log)
					}
				})
			}
		})
	}
}
