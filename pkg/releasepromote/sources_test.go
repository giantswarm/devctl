package releasepromote

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestForbiddenHint(t *testing.T) {
	hint := ForbiddenHint(authstore.Token{Source: authstore.SourceKeychain})
	require.Contains(t, hint, "Actions write")
	require.Contains(t, hint, "$DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN, $OPSCTL_GITHUB_TOKEN")
	require.Empty(t, ForbiddenHint(authstore.Token{Source: "$GITHUB_TOKEN"}))
}
