package workflows

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
// (valkey-app-79) and asserts what cliff.toml's [remote.github].repo names,
// the auto-release workflow's own way of reading the repository's name.
func Test_Run_RepoName(t *testing.T) {
	testCases := []struct {
		name            string
		releaseWorkflow string
		remote          string
		repoNameFlag    string
		wantRepo        string
		wantErr         string
	}{
		{
			name:            "origin remote names the repository",
			releaseWorkflow: releaseWorkflowAutoRelease,
			remote:          "https://github.com/giantswarm/valkey-app.git",
			wantRepo:        `repo = "valkey-app"`,
		},
		{
			name:            "flag wins over the origin remote",
			releaseWorkflow: releaseWorkflowAutoRelease,
			remote:          "https://github.com/giantswarm/valkey-app.git",
			repoNameFlag:    "valkey-exporter",
			wantRepo:        `repo = "valkey-exporter"`,
		},
		{
			name:            "flag without an origin remote",
			releaseWorkflow: releaseWorkflowAutoRelease,
			repoNameFlag:    "valkey-app",
			wantRepo:        `repo = "valkey-app"`,
		},
		{
			name:            "origin remote outside giantswarm",
			releaseWorkflow: releaseWorkflowAutoRelease,
			remote:          "git@github.com:teemow/valkey-app.git",
			wantErr:         "pass --repo-name <name>",
		},
		{
			name:            "no origin remote",
			releaseWorkflow: releaseWorkflowAutoRelease,
			wantErr:         "pass --repo-name <name>",
		},
		{
			name: "legacy release workflow needs no name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "valkey-app-79")
			git(t, "init", "--quiet", dir)
			if tc.remote != "" {
				git(t, "-C", dir, "remote", "add", "origin", tc.remote)
			}
			t.Chdir(dir)

			releaseWorkflow := tc.releaseWorkflow
			if releaseWorkflow == "" {
				releaseWorkflow = releaseWorkflowLegacy
			}
			r := &runner{flag: &flag{
				Flavours:             gen.FlavourSlice{gen.FlavourGeneric},
				Language:             "go",
				RunSecurityScorecard: true,
				ReleaseWorkflow:      releaseWorkflow,
				RepoName:             tc.repoNameFlag,
			}}
			err := r.Run(&cobra.Command{}, nil)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.NoFileExists(t, "cliff.toml")
				return
			}
			require.NoError(t, err)

			if tc.wantRepo == "" {
				require.NoFileExists(t, "cliff.toml")
				return
			}
			got, err := os.ReadFile("cliff.toml")
			require.NoError(t, err)
			require.Contains(t, string(got), tc.wantRepo)
		})
	}
}

// Test_Run_ForkLine pins what a fork line gets: nothing on the legacy release
// workflow, and the release flow alone on auto-release, cut from the branch
// the line is consumed from.
func Test_Run_ForkLine(t *testing.T) {
	for _, tc := range []struct {
		releaseWorkflow string
		want            []string
	}{
		{releaseWorkflow: releaseWorkflowLegacy},
		{
			releaseWorkflow: releaseWorkflowAutoRelease,
			want: []string{
				".github/workflows/zz_generated.auto_release.yaml",
				".github/workflows/zz_generated.semantic_pull_request.yaml",
				"cliff.toml",
			},
		},
	} {
		t.Run(tc.releaseWorkflow, func(t *testing.T) {
			t.Chdir(t.TempDir())

			r := &runner{flag: &flag{
				Flavours:        gen.FlavourSlice{gen.FlavourFork},
				Language:        "go",
				ReleaseBranch:   "giantswarm",
				ReleaseWorkflow: tc.releaseWorkflow,
				RepoName:        "upstream-fork",
			}}
			require.NoError(t, r.Run(&cobra.Command{}, nil))

			var got []string
			err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					got = append(got, path)
				}
				return err
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			if tc.want != nil {
				workflow, err := os.ReadFile(".github/workflows/zz_generated.auto_release.yaml")
				require.NoError(t, err)
				require.Contains(t, string(workflow), "      - giantswarm\n")
			}
		})
	}
}

func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // the arguments are the test's own
	require.NoError(t, err, string(out))
}
