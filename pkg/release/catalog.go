package release

import (
	"regexp"
	"strings"
)

// gitSHASuffix matches a version string ending with a full-length git commit
// hash, e.g. "7.3.0-abc123...40chars". A full SHA is exactly 40 hex characters.
var gitSHASuffix = regexp.MustCompile(`-[0-9a-f]{40}$`)

// ciBuildSuffix matches a version string ending with a CI dev-build suffix as
// produced by the Giant Swarm build pipeline, e.g.
// "2.12.2-r5aa25357t20261024082658h446cfd7" (r<short SHA>t<timestamp>h<short SHA>).
var ciBuildSuffix = regexp.MustCompile(`-r[0-9a-f]{7,10}t[0-9]{14}h[0-9a-f]{7,10}$`)

// releaseVersion matches a plain release version without any pre-release or
// build suffix, e.g. "3.9.2" or "v3.9.2".
var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

// isDevVersion returns true when the version string ends with a git commit
// hash suffix (e.g. "7.3.0-abc123...40chars") or a CI dev-build suffix
// (e.g. "2.12.2-r5aa25357t20261024082658h446cfd7"), indicating a development build.
func isDevVersion(version string) bool {
	return gitSHASuffix.MatchString(version) || ciBuildSuffix.MatchString(version)
}

// isReleaseVersion returns true when the version string is a plain release
// version without any pre-release or dev suffix (e.g. "3.9.2").
func isReleaseVersion(version string) bool {
	return releaseVersion.MatchString(version)
}

// toTestCatalog returns the test-catalog value for the given catalog value.
// Strips any trailing "-catalog" suffix, then appends "-test"
// (e.g. "cluster" → "cluster-test", "default" → "default-test").
// An empty value maps to "default-test".
// Already-test values (suffix "-test") are returned unchanged.
func toTestCatalog(catalog string) string {
	if catalog == "" {
		return "default-test"
	}
	if strings.HasSuffix(catalog, "-test") {
		return catalog
	}
	return strings.TrimSuffix(catalog, "-catalog") + "-test"
}

// fromTestCatalog reverses toTestCatalog: strips a trailing "-test" suffix
// (e.g. "cluster-test" → "cluster"). "default-test" maps back to the empty
// value, which means the default catalog. Non-test values are returned unchanged.
func fromTestCatalog(catalog string) string {
	if !strings.HasSuffix(catalog, "-test") {
		return catalog
	}
	base := strings.TrimSuffix(catalog, "-test")
	if base == "default" {
		return ""
	}
	return base
}
