package renovate

import (
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
)

// A custom preset is optional only when absent. Failure to inspect it must
// leave both possible generated configs and Dependabot untouched.
func Test_Run_CustomConfigDiscovery(t *testing.T) {
	for _, fork := range []bool{false, true} {
		name := "regular"
		if fork {
			name = "fork"
		}
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"directory", "symlink loop", "file symlink"} {
				t.Run(kind, func(t *testing.T) {
					t.Chdir(t.TempDir())
					const custom = "renovate-custom.json5"
					if kind == "directory" {
						require.NoError(t, os.Mkdir(custom, 0o750))
					} else {
						target := custom
						if kind == "file symlink" {
							target = "fixture-custom.json5"
							require.NoError(t, os.WriteFile(target, []byte("{enabled: false}\n"), 0o600))
						}
						err := os.Symlink(target, custom)
						if err != nil && runtime.GOOS == "windows" {
							t.Skipf("symlinks unavailable: %v", err)
						}
						require.NoError(t, err)
					}
					require.NoError(t, os.Mkdir(".github", 0o750))
					files := map[string]string{
						"renovate.json5":         "{\"extends\":[\"github>giantswarm/fixture:renovate-custom.json5\"]}\n",
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
					args := []string{"--language=go", "--repo-name=fixture"}
					if fork {
						args = append(args, "--flavour=fork")
					}
					cmd.SetArgs(args)
					err = cmd.Execute()
					if kind == "file symlink" {
						require.NoError(t, err)
						configFile := "renovate.json5"
						if fork {
							configFile = "renovate.json"
						}
						got, err := os.ReadFile(configFile) // #nosec G304 -- one of the two fixed config names
						require.NoError(t, err)
						require.Contains(t, string(got), "github>giantswarm/fixture:renovate-custom.json5")
						got, err = os.ReadFile(custom)
						require.NoError(t, err)
						require.Equal(t, "{enabled: false}\n", string(got))
						target, err := os.Readlink(custom)
						require.NoError(t, err)
						require.Equal(t, "fixture-custom.json5", target)
						return
					}
					require.ErrorContains(t, err, custom)
					for path, content := range files {
						got, err := os.ReadFile(path) // #nosec G304 -- fixed fixture paths in a temporary directory
						require.NoError(t, err)
						require.Equal(t, content, string(got), path)
					}
				})
			}
		})
	}
}
