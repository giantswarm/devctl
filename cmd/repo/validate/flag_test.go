package validate

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

func TestModeDefaults(t *testing.T) {
	f := &flag{TeamFile: "repositories/team-bumblebee.yaml"}
	require.NoError(t, f.Validate())
	require.Equal(t, reposetup.ModeExisting, f.mode(), "a whole file is on main already")

	f.Entries = []string{"my-service"}
	require.Equal(t, reposetup.ModeCreate, f.mode(), "the entries named are being added")

	f.Mode = "existing"
	require.NoError(t, f.Validate())
	require.Equal(t, reposetup.ModeExisting, f.mode(), "--mode wins")

	f.Mode = "repair"
	err := f.Validate()
	require.True(t, IsInvalidFlag(err), "%v", err)
	require.Contains(t, err.Error(), "want create or existing")
}
