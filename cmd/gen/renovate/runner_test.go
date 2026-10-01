package renovate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
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

func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command("git", args...).CombinedOutput() //nolint:gosec // the arguments are the test's own
	require.NoError(t, err, string(out))
}
