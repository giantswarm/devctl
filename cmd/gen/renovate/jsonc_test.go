package renovate

import (
	"io"
	"os"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
)

// Renovate reads renovate.jsonc before renovate.json5. Generating json5 while
// leaving jsonc in place cannot apply the requested configuration.
func Test_Run_RejectsShadowingJSONC(t *testing.T) {
	for _, existingOutput := range []bool{false, true} {
		name := "initial generation"
		if existingOutput {
			name = "regeneration"
		}
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.Mkdir(".github", 0o750))
			files := map[string]string{
				"renovate.jsonc":         "{\"dependencyDashboard\": true}\n",
				"renovate.json":          "{\"enabled\": false}\n",
				".github/dependabot.yml": "version: 2\n",
			}
			if existingOutput {
				files["renovate.json5"] = "{dependencyDashboard: false}\n"
			}
			for path, content := range files {
				require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			}

			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs([]string{"--language=go", "--repo-name=fixture", "--deprecated"})
			err = cmd.Execute()
			require.ErrorContains(t, err, "renovate.jsonc")
			require.True(t, IsInvalidFlag(err))
			for path, content := range files {
				got, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths in a temporary directory
				require.NoError(t, err)
				require.Equal(t, content, string(got), path)
			}
			if !existingOutput {
				require.NoFileExists(t, "renovate.json5")
			}
		})
	}
}
