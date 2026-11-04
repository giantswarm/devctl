package renovate

import (
	"io"
	"os"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/stretchr/testify/require"
	"github.com/titanous/json5"
)

// Test_Run_CustomConfigReadyBeforeRules exercises the first generation, a
// later custom-rule edit without regeneration, and alignment after that edit.
func Test_Run_CustomConfigReadyBeforeRules(t *testing.T) {
	const customRules = "// Repo-owned rule: hold an incompatible image update.\n{ packageRules: [{ matchPackageNames: ['example/base-image'], allowedVersions: '<2.0.0', automerge: false }] }\n"
	testCases := []struct {
		name     string
		fork     bool
		existing bool
	}{
		{name: "case 0: initialize a normal repository"},
		{name: "case 1: preserve a normal repository's rules", existing: true},
		{name: "case 2: initialize a fork", fork: true},
		{name: "case 3: preserve a fork's rules", fork: true, existing: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.existing {
				require.NoError(t, os.WriteFile("renovate-custom.json5", []byte(customRules), 0o600))
			}
			logger, err := micrologger.New(micrologger.Config{})
			require.NoError(t, err)
			args := []string{"--language", "go", "--repo-name", "example-repo", "--circleci-generated", "--deprecated", "--extends", "flux.json5"}
			configFile := "renovate.json5"
			if tc.fork {
				args = append(args, "--flavour", "fork")
				configFile = "renovate.json"
			}
			run := func() {
				t.Helper()
				cmd, err := New(Config{Logger: logger, Stdout: io.Discard, Stderr: io.Discard})
				require.NoError(t, err)
				cmd.SetArgs(args)
				require.NoError(t, cmd.Execute())
			}
			run()

			root, err := os.ReadFile(configFile) // #nosec G304 -- one of the two fixed config names in t.TempDir()
			require.NoError(t, err)
			var parsed struct {
				Extends []string `json:"extends"`
			}
			require.NoError(t, json5.Unmarshal(root, &parsed))
			require.Equal(t, []string{
				"github>giantswarm/renovate-presets:default.json5",
				"github>giantswarm/renovate-presets:lang-go.json5",
				"github>giantswarm/renovate-presets:tests-ats.json5",
				"github>giantswarm/renovate-presets:flux.json5",
				"github>giantswarm/renovate-presets:deprecated.json5",
				"github>giantswarm/example-repo:renovate-custom.json5",
			}, parsed.Extends)
			custom, err := os.ReadFile("renovate-custom.json5")
			require.NoError(t, err)
			if tc.existing {
				require.Equal(t, customRules, string(custom))
			} else {
				var empty map[string]interface{}
				require.NoError(t, json5.Unmarshal(custom, &empty))
				require.NotNil(t, empty)
				require.Empty(t, empty)
			}

			// Only the preset changes in the later PR. The existing root
			// already references it, so that PR needs no regeneration.
			edited := []byte("// Keep this exact formatting.\n" + customRules)
			require.NoError(t, os.WriteFile("renovate-custom.json5", edited, 0o600))
			unchanged, err := os.ReadFile(configFile) // #nosec G304 -- the same fixed config name
			require.NoError(t, err)
			require.Equal(t, root, unchanged)

			run()
			preserved, err := os.ReadFile("renovate-custom.json5")
			require.NoError(t, err)
			require.Equal(t, edited, preserved)
			aligned, err := os.ReadFile(configFile) // #nosec G304 -- the same fixed config name
			require.NoError(t, err)
			require.Equal(t, root, aligned)
		})
	}
}
