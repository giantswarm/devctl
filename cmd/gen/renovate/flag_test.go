package renovate

import (
	"io"
	"os"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
)

// An explicit repo name must be a name under giantswarm, just like a name
// discovered from origin. It cannot inject preset syntax or break JSON5.
func Test_Run_RejectsInvalidRepoName(t *testing.T) {
	for _, name := range []string{"giantswarm/fixture", "fixture'", "fixture#other-branch", "fixture:other.json5"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.Mkdir(".github", 0o750))
			files := map[string]string{
				"renovate-custom.json5":  "{enabled: false}\n",
				"renovate.json5":         "{dependencyDashboard: true}\n",
				"renovate.json":          "{\"dependencyDashboard\":true}\n",
				".github/dependabot.yml": "version: 2\n",
			}
			for path, content := range files {
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			}

			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SetArgs([]string{"--language=go", "--repo-name=" + name})
			err = cmd.Execute()
			require.ErrorContains(t, err, "--repo-name")
			require.True(t, IsInvalidFlag(err))
			for path, content := range files {
				got, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths in a temporary directory
				require.NoError(t, err)
				require.Equal(t, content, string(got), path)
			}
		})
	}
}
