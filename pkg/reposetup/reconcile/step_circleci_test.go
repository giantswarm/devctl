package reconcile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsReleaseTag: the release step verifies a vX.Y.Z tag, a pre-release
// suffix allowed — what auto-release cuts and the generated pipeline's tag
// filter builds — and nothing tagged otherwise.
func TestIsReleaseTag(t *testing.T) {
	cases := map[string]bool{
		"v0.1.0":         true,
		"v10.20.30":      true,
		"v1.2.3-rc.1":    true,
		"v1.2.3-dev.abc": true,
		"0.1.0":          false, // auto-release cuts the v
		"v0.1":           false, // not a full version
		"v01.2.3":        false, // a leading zero is no semver
		"base/v0.1.0":    false, // a per-component tag
		"release-1.2.3":  false,
		"":               false,
	}
	for tag, want := range cases {
		t.Run(tag, func(t *testing.T) {
			require.Equal(t, want, isReleaseTag(tag))
		})
	}
}
