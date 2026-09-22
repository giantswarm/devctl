package setlifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

// The lifecycle is one of three words, and a deletion needs the name typed
// again.
func TestValidateLifecycle(t *testing.T) {
	f := &flag{}
	require.NoError(t, f.validateLifecycle("x", "archived"))
	require.True(t, client.IsInvalidFlag(f.validateLifecycle("x", "production")))
	err := f.validateLifecycle("scratch", "deleted")
	require.True(t, client.IsInvalidFlag(err))
	require.Contains(t, err.Error(), "--confirm scratch")
	f.Confirm = "scratch"
	require.NoError(t, f.validateLifecycle("scratch", "deleted"))
}
