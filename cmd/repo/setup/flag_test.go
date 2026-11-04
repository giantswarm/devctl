package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func setupFlags(t *testing.T, args ...string) *flag {
	t.Helper()
	f := &flag{}
	cmd := &cobra.Command{}
	f.Init(cmd)
	require.NoError(t, cmd.ParseFlags(args))
	return f
}
