package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_mergeClaudeSettings(t *testing.T) {
	testCases := []struct {
		name     string
		existing string
		expected string
	}{
		{
			name:     "no file",
			existing: "",
			expected: `{
  "enabledPlugins": {"gs-plans@giantswarm": true},
  "extraKnownMarketplaces": {"giantswarm": {"source": {"source": "github", "repo": "giantswarm/claude-code"}}}
}`,
		},
		{
			name: "team settings are kept",
			existing: `{
  "permissions": {"allow": ["Bash(gh issue view:*)"]},
  "enabledPlugins": {"gs-product@giantswarm": true, "gs-plans@giantswarm": false},
  "extraKnownMarketplaces": {"other": {"source": {"source": "github", "repo": "example/other"}}}
}`,
			expected: `{
  "permissions": {"allow": ["Bash(gh issue view:*)"]},
  "enabledPlugins": {"gs-product@giantswarm": true, "gs-plans@giantswarm": true},
  "extraKnownMarketplaces": {
    "other": {"source": {"source": "github", "repo": "example/other"}},
    "giantswarm": {"source": {"source": "github", "repo": "giantswarm/claude-code"}}
  }
}`,
		},
		{
			name:     "non-object values are replaced",
			existing: `{"enabledPlugins": [], "extraKnownMarketplaces": "x"}`,
			expected: `{
  "enabledPlugins": {"gs-plans@giantswarm": true},
  "extraKnownMarketplaces": {"giantswarm": {"source": {"source": "github", "repo": "giantswarm/claude-code"}}}
}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeClaudeSettings([]byte(tc.existing))
			require.NoError(t, err)
			require.JSONEq(t, tc.expected, string(got))
			require.Equal(t, byte('\n'), got[len(got)-1])

			again, err := mergeClaudeSettings(got)
			require.NoError(t, err)
			require.Equal(t, string(got), string(again))
		})
	}
}

func Test_mergeClaudeSettings_InvalidJSON(t *testing.T) {
	_, err := mergeClaudeSettings([]byte(`["not", "an", "object"]`))
	require.ErrorIs(t, err, invalidSettingsError)
}

func Test_NewClaudeSettingsInput_ReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"model": "sonnet"}`), 0600))

	got, err := NewClaudeSettingsInput(path).Generate(t.Context())
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(got, &settings))
	require.Equal(t, "sonnet", settings["model"])
	require.Equal(t, map[string]any{"gs-plans@giantswarm": true}, settings["enabledPlugins"])
}
