package renovate

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"

	pkgrenovate "github.com/giantswarm/devctl/v8/pkg/renovate"
)

// Every configuration location recognized by devctl can carry a fork's
// opt-out. A newly generated root config would take precedence over it.
func Test_Run_ForkPreservesConfigOptOutPaths(t *testing.T) {
	for _, path := range pkgrenovate.ConfigPaths {
		t.Run(path, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
			const optOut = "{\n  \"enabled\": false\n}\n"
			require.NoError(t, os.WriteFile(path, []byte(optOut), 0o600))
			require.NoError(t, os.MkdirAll(".github", 0o750))
			const dependabot = "version: 2\n"
			require.NoError(t, os.WriteFile(".github/dependabot.yml", []byte(dependabot), 0o600))

			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			require.NoError(t, err)
			cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
			require.NoError(t, err)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs([]string{"--language=go", "--flavour=fork"})
			err = cmd.Execute()
			require.ErrorContains(t, err, path+" turns Renovate off for this fork line")
			require.True(t, IsInvalidFlag(err))

			got, err := os.ReadFile(path) // #nosec G304 -- fixed config names in a temporary directory
			require.NoError(t, err)
			require.Equal(t, optOut, string(got))
			got, err = os.ReadFile(".github/dependabot.yml")
			require.NoError(t, err)
			require.Equal(t, dependabot, string(got))
			for _, generated := range []string{"renovate.json", "renovate.json5"} {
				if generated != path {
					require.NoFileExists(t, generated)
				}
			}
		})
	}
}
