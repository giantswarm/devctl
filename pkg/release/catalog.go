package release

import (
	"strings"

	"github.com/blang/semver"
)

// isDevVersion returns true when the version string carries a semver pre-release
// identifier (e.g. "7.3.0-abc123sha"), indicating a development build.
func isDevVersion(version string) bool {
	v, err := semver.ParseTolerant(version)
	if err != nil {
		return false
	}
	return len(v.Pre) > 0
}

// toTestCatalog returns the test-catalog value for the given catalog value.
// The caller must normalise empty strings to the appropriate type default
// ("default" for apps, "control-plane-catalog" for components) before calling.
// Strips any trailing "-catalog" suffix, then appends "-test".
// Already-test values (suffix "-test") are returned unchanged.
func toTestCatalog(catalog string) string {
	if strings.HasSuffix(catalog, "-test") {
		return catalog
	}
	return strings.TrimSuffix(catalog, "-catalog") + "-test"
}
