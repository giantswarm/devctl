package releasewait

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
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

// releaseTagPattern is a release tag as auto-release cuts it: a stable
// vX.Y.Z or a candidate vX.Y.Z-rc.N, the leading v optional. An upstream
// pre-release (v1.2.0-alpha.1) names no release of the repository.
var releaseTagPattern = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)(-rc\.[0-9]+)?$`)

// releaseTag is the tag the wait follows among tags, the tags on the commit
// sha of repository: the newest release tag, else the newest tag. A candidate and its promoted stable share a
// commit by design; release tags of two versions on one commit are a
// conflict no reader can resolve, and the caller names the version.
func releaseTag(repository, sha string, tags []string) (string, error) {
	var release []string
	versions := map[string]bool{}
	for _, tag := range tags {
		if m := releaseTagPattern.FindStringSubmatch(tag); m != nil {
			release = append(release, tag)
			versions[m[1]] = true
		}
	}
	if len(versions) > 1 {
		slices.SortFunc(release, func(a, b string) int {
			return semver.MustParse(b).Compare(semver.MustParse(a))
		})
		return "", &AmbiguousReleaseError{
			Tags:   release,
			Reason: fmt.Sprintf("commit %s of %s carries the release tags of %d versions (%s): release wait does not choose one; name the version, devctl release wait %s %s", short(sha), repository, len(versions), strings.Join(release, ", "), repository, release[0]),
		}
	}
	if len(release) > 0 {
		return githubclient.NewestTag(release), nil
	}
	return githubclient.NewestTag(tags), nil
}
