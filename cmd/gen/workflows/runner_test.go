package workflows

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
			// A repository generated while devctl shipped the Changelog
			// check carries it: every gen run deletes it.
			require.NoError(t, os.MkdirAll(".github/workflows", 0o750))
			require.NoError(t, os.WriteFile(".github/workflows/zz_generated.changelog.yaml", nil, 0o600))

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
			require.NoFileExists(t, ".github/workflows/zz_generated.changelog.yaml")

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

// Test_Run_AutoReleaseDeclaredLegacy pins what a repository that carries the
// Auto Release files gets: a legacy run deletes them and names the
// declaration as the place to enable Auto Release, its validate-changelog
// workflow refuses them on a pull request, and an auto-release run keeps
// them without a warning.
func Test_Run_AutoReleaseDeclaredLegacy(t *testing.T) {
	for _, tc := range []struct {
		releaseWorkflow string
		wantWarning     bool
	}{
		{releaseWorkflow: releaseWorkflowLegacy, wantWarning: true},
		{releaseWorkflow: releaseWorkflowAutoRelease},
	} {
		t.Run(tc.releaseWorkflow, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".github/workflows", 0o750))
			require.NoError(t, os.WriteFile(".github/workflows/zz_generated.auto_release.yaml", []byte("name: Auto Release\n"), 0o600))
			require.NoError(t, os.WriteFile("cliff.toml", []byte("[changelog]\n"), 0o600))

			var stderr bytes.Buffer
			r := &runner{
				flag: &flag{
					Flavours:        gen.FlavourSlice{gen.FlavourGeneric},
					ReleaseBranch:   "main",
					ReleaseWorkflow: tc.releaseWorkflow,
					RepoName:        "app-catalog-cleanup-tool",
				},
				stderr: &stderr,
			}
			require.NoError(t, r.Run(&cobra.Command{}, nil))

			if !tc.wantWarning {
				require.Empty(t, stderr.String())
				require.FileExists(t, ".github/workflows/zz_generated.auto_release.yaml")
				require.FileExists(t, "cliff.toml")
				return
			}

			require.Contains(t, stderr.String(), "deleting .github/workflows/zz_generated.auto_release.yaml and cliff.toml")
			require.Contains(t, stderr.String(), "gen.ci.releaseWorkflow: auto-release")
			require.NoFileExists(t, ".github/workflows/zz_generated.auto_release.yaml")
			require.NoFileExists(t, "cliff.toml")

			workflow, err := os.ReadFile(".github/workflows/zz_generated.validate_changelog.yaml")
			require.NoError(t, err)
			require.Contains(t, string(workflow), "      - '.github/workflows/zz_generated.auto_release.yaml'\n      - 'cliff.toml'\n")
			require.Contains(t, string(workflow), "set gen.ci.releaseWorkflow: auto-release there")
		})
	}
}

func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // the arguments are the test's own
	require.NoError(t, err, string(out))
}

// Test_Run_FleetMctl pins what a management-clusters repository (flavour
// fleet) gets: the mctl workflows under zz_generated.mctl_*, and the
// unprefixed copies and op.env `mctl create cmc-repo` wrote deleted. The
// deletions are fleet-only: another flavour's own config.yaml stays.
func Test_Run_FleetMctl(t *testing.T) {
	mctlWorkflows := []string{"close-cascade", "config", "create-mc", "init", "merge-cascade", "merge-gate", "patch-init", "validate-config", "validate-manifest"}
	legacy := []string{"op.env"}
	for _, name := range mctlWorkflows {
		legacy = append(legacy, name+".yaml")
	}

	for _, flavour := range []gen.Flavour{gen.FlavourManagementClustersFleet, gen.FlavourGeneric} {
		t.Run(flavour.String(), func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(".github/workflows", 0o750))
			for _, name := range append(legacy, "lint.yaml") {
				require.NoError(t, os.WriteFile(filepath.Join(".github/workflows", name), []byte("name: x\n"), 0o600))
			}

			r := &runner{flag: &flag{
				Flavours:        gen.FlavourSlice{flavour},
				Language:        "generic",
				ReleaseWorkflow: releaseWorkflowLegacy,
			}}
			require.NoError(t, r.Run(&cobra.Command{}, nil))

			require.FileExists(t, ".github/workflows/lint.yaml")
			for _, name := range legacy {
				if flavour == gen.FlavourManagementClustersFleet {
					require.NoFileExists(t, filepath.Join(".github/workflows", name))
				} else {
					require.FileExists(t, filepath.Join(".github/workflows", name))
				}
			}
			for _, name := range mctlWorkflows {
				generated := filepath.Join(".github/workflows", "zz_generated.mctl_"+strings.ReplaceAll(name, "-", "_")+".yaml")
				if flavour == gen.FlavourManagementClustersFleet {
					require.FileExists(t, generated)
				} else {
					require.NoFileExists(t, generated)
				}
			}
		})
	}
}
