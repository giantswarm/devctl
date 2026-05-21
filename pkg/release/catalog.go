package release

import (
	"regexp"
	"strings"
)

// gitSHASuffix matches a version string ending with a full-length git commit
// hash, e.g. "7.3.0-abc123...40chars". A full SHA is exactly 40 hex characters.
var gitSHASuffix = regexp.MustCompile(`-[0-9a-f]{40}$`)

// isDevVersion returns true when the version string ends with a git commit
// hash suffix (e.g. "7.3.0-abc123f"), indicating a development build.
func isDevVersion(version string) bool {
	return gitSHASuffix.MatchString(version)
}

// toTestCatalog returns the test-catalog value for the given catalog value.
// Strips any trailing "-catalog" suffix, then appends "-test"
// (e.g. "cluster" → "cluster-test", "default" → "default-test").
// An empty value maps to "default-test".
// Already-test values (suffix "-test") are returned unchanged.
func toTestCatalog(catalog string) string {
	if strings.HasSuffix(catalog, "-test") {
		return catalog
	}
	return strings.TrimSuffix(catalog, "-catalog") + "-test"
}
