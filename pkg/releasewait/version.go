package releasewait

import (
	"regexp"
	"strings"
)

// versionPattern is a semantic version with an optional leading v and an
// optional pre-release, the tags auto-release and the legacy workflow cut.
var versionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// Version is a release version as given on the command line.
type Version struct {
	// Given is the argument as typed.
	Given string
	// Bare is the version without a leading v: the tag of the image and the
	// chart.
	Bare string
}

// ParseVersion accepts vX.Y.Z and X.Y.Z, with a pre-release suffix.
func ParseVersion(s string) (Version, error) {
	if !versionPattern.MatchString(s) {
		return Version{}, usageErr("%q is not a version: expected vX.Y.Z or X.Y.Z", s)
	}
	return Version{Given: s, Bare: strings.TrimPrefix(s, "v")}, nil
}

// Tags are the tag names the version may exist under, the given spelling
// first: a repository tags vX.Y.Z or X.Y.Z, and the caller need not know
// which.
func (v Version) Tags() []string {
	if strings.HasPrefix(v.Given, "v") {
		return []string{v.Given, v.Bare}
	}
	return []string{v.Given, "v" + v.Bare}
}
