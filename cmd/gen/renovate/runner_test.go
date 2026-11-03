package renovate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// Test_Run_RepoName runs the command in a checkout named like a worktree
// (valkey-app-79) and asserts which repository the renovate-custom.json5
// extends entry names.
func Test_Run_RepoName(t *testing.T) {
	testCases := []struct {
		name         string
		remote       string
		repoNameFlag string
		customConfig bool
		wantExtends  string
		wantErr      string
	}{
		{
			name:         "origin remote names the repository",
			remote:       "https://github.com/giantswarm/valkey-app.git",
			customConfig: true,
			wantExtends:  "'github>giantswarm/valkey-app:renovate-custom.json5'",
		},
		{
			name:         "flag wins over the origin remote",
			remote:       "https://github.com/giantswarm/valkey-app.git",
			repoNameFlag: "valkey-exporter",
			customConfig: true,
			wantExtends:  "'github>giantswarm/valkey-exporter:renovate-custom.json5'",
		},
		{
			name:         "flag without an origin remote",
			repoNameFlag: "valkey-app",
			customConfig: true,
			wantExtends:  "'github>giantswarm/valkey-app:renovate-custom.json5'",
		},
		{
			name:         "origin remote outside giantswarm",
			remote:       "git@github.com:teemow/valkey-app.git",
			customConfig: true,
			wantErr:      "pass --repo-name <name>",
		},
		{
			name:         "no origin remote",
			customConfig: true,
			wantErr:      "pass --repo-name <name>",
		},
		{
			name: "no renovate-custom.json5 needs no name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "valkey-app-79")
			git(t, "init", "--quiet", dir)
			if tc.remote != "" {
				git(t, "-C", dir, "remote", "add", "origin", tc.remote)
			}
			if tc.customConfig {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "renovate-custom.json5"), []byte("{}\n"), 0o600))
			}
			t.Chdir(dir)

			r := &runner{flag: &flag{Language: "go", RepoName: tc.repoNameFlag}}
			err := r.Run(&cobra.Command{}, nil)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.NoFileExists(t, "renovate.json5")
				return
			}
			require.NoError(t, err)

			got, err := os.ReadFile("renovate.json5")
			require.NoError(t, err)
			require.NotContains(t, string(got), "valkey-app-79")
			if tc.wantExtends != "" {
				require.Contains(t, string(got), tc.wantExtends)
			} else {
				require.NotContains(t, string(got), "renovate-custom.json5'")
			}
		})
	}
}

// Test_Run_Fork asserts which file each flavour writes and removes, and that a
// fork line's opt-out stays as it is.
func Test_Run_Fork(t *testing.T) {
	testCases := []struct {
		name      string
		flavours  gen.FlavourSlice
		existing  map[string]string
		wantFile  string
		wantGone  string
		wantErr   string
		wantForks bool
	}{
		{
			name:      "a fork gets renovate.json, the hand-written renovate.json5 goes",
			flavours:  gen.FlavourSlice{gen.FlavourFork},
			existing:  map[string]string{"renovate.json5": "{\n  dependencyDashboard: true,\n}\n"},
			wantFile:  "renovate.json",
			wantGone:  "renovate.json5",
			wantForks: true,
		},
		{
			name:     "another flavour keeps renovate.json5, renovate.json goes",
			flavours: gen.FlavourSlice{gen.FlavourApp},
			existing: map[string]string{"renovate.json": "{}\n"},
			wantFile: "renovate.json5",
			wantGone: "renovate.json",
		},
		{
			name:     "a fork line's opt-out in renovate.json5 stays",
			flavours: gen.FlavourSlice{gen.FlavourFork},
			existing: map[string]string{"renovate.json5": "{\n  reviewers: ['teemow'],\n  enabled: false,\n}\n"},
			wantErr:  "renovate.json5 turns Renovate off for this fork line",
		},
		{
			name:     "a fork line's opt-out in renovate.json stays",
			flavours: gen.FlavourSlice{gen.FlavourFork},
			existing: map[string]string{"renovate.json": "{\n  \"enabled\": false\n}\n"},
			wantErr:  "renovate.json turns Renovate off for this fork line",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.Mkdir(".github", 0o750))
			require.NoError(t, os.WriteFile(".github/dependabot.yml", []byte("version: 2\n"), 0o600))
			for name, content := range tc.existing {
				require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
			}

			r := &runner{flag: &flag{Language: "go", Flavours: tc.flavours}}
			err := r.Run(&cobra.Command{}, nil)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				for name, content := range tc.existing {
					got, err := os.ReadFile(name) // #nosec G304 -- the test's own file
					require.NoError(t, err)
					require.Equal(t, content, string(got))
				}
				return
			}
			require.NoError(t, err)

			got, err := os.ReadFile(tc.wantFile)
			require.NoError(t, err)
			require.NoFileExists(t, tc.wantGone)
			if tc.wantForks {
				require.Contains(t, string(got), `"forkProcessing": "enabled",`)
				// A fork line's dependabot.yml is upstream's.
				require.FileExists(t, ".github/dependabot.yml")
			} else {
				require.NotContains(t, string(got), "forkProcessing")
				require.NoFileExists(t, ".github/dependabot.yml")
			}
		})
	}
}

func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // the arguments are the test's own
	require.NoError(t, err, string(out))
}
