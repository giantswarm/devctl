package adopt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
)

// The entry carries the fields that are set and nothing else; gen appears
// only with a flavour, a language or the CI switch.
func TestEntry(t *testing.T) {
	f := &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}, Team: "team-bumblebee"}
	require.NoError(t, f.Validate())
	require.Equal(t, map[string]any{}, f.entry())

	f.ComponentType, f.Language, f.Flavours, f.CIGenerate, f.Align, f.Lifecycle = "tool", "go", []string{"cli"}, "false", true, "deprecated"
	require.NoError(t, f.Validate())
	require.Equal(t, map[string]any{
		"componentType": "tool", "lifecycle": "deprecated", "align": true,
		"gen": map[string]any{"flavours": []string{"cli"}, "language": "go", "ci": map[string]any{"generate": false}},
	}, f.entry())
}

// The team is required; the CI switch and the lifecycle take their words only.
func TestValidate(t *testing.T) {
	f := &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}}
	require.True(t, client.IsInvalidFlag(f.Validate()))
	f.Team = "team-bumblebee"
	f.CIGenerate = "yes"
	require.True(t, client.IsInvalidFlag(f.Validate()))
	f.CIGenerate = ""
	f.Lifecycle = "deleted"
	err := f.Validate()
	require.True(t, client.IsInvalidFlag(err))
	require.Contains(t, err.Error(), "set-lifecycle")
}
