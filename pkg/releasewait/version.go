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

// imageTagPattern is a release tag of a multi-image repository, as its
// auto-release cuts it: <name>/vX.Y.Z, the name an image directory under
// toolchains/ and a valid image repository path component.
var imageTagPattern = regexp.MustCompile(`^([a-z0-9]+(?:[._-][a-z0-9]+)*)/v([0-9]+\.[0-9]+\.[0-9]+)$`)

// Version is a release version as given on the command line.
type Version struct {
	// Given is the argument as typed.
	Given string
	// Bare is the version without a leading v (and image name): the tag of
	// the image and the chart.
	Bare string
	// Image is the image name of a multi-image release, <name>/vX.Y.Z;
	// empty for any other version.
	Image string
}

// ParseVersion accepts vX.Y.Z and X.Y.Z, with a pre-release suffix, and a
// multi-image release <name>/vX.Y.Z.
func ParseVersion(s string) (Version, error) {
	if image, bare, ok := imageTag(s); ok {
		return Version{Given: s, Bare: bare, Image: image}, nil
	}
	if !versionPattern.MatchString(s) {
		return Version{}, usageErr("%q is not a version: expected vX.Y.Z, X.Y.Z or <image>/vX.Y.Z", s)
	}
	return Version{Given: s, Bare: strings.TrimPrefix(s, "v")}, nil
}

// imageTag splits a multi-image release tag <name>/vX.Y.Z into the image
// name and the bare version; ok is false for any other tag.
func imageTag(tag string) (image, bare string, ok bool) {
	m := imageTagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// tagVersion is the version of a release tag as its artifacts carry it:
// X.Y.Z of vX.Y.Z, X.Y.Z and <name>/vX.Y.Z, and the image name of the last.
func tagVersion(tag string) (image, version string) {
	if image, bare, ok := imageTag(tag); ok {
		return image, bare
	}
	return "", strings.TrimPrefix(tag, "v")
}

// Tags are the tag names the version may exist under, the given spelling
// first: a repository tags vX.Y.Z or X.Y.Z, and the caller need not know
// which. A multi-image release exists under its one spelling.
func (v Version) Tags() []string {
	if v.Image != "" {
		return []string{v.Given}
	}
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
// sha of repository: the newest release tag, else the one multi-image
// release tag (<name>/vX.Y.Z), else the newest tag. A candidate and its promoted stable share a
// commit by design; release tags of two versions on one commit are a
// conflict no reader can resolve, and the caller names the version. So are
// the releases of two images on one commit (a change to the build files the
// images share releases every image): each is waited for by its tag.
func releaseTag(repository, sha string, tags []string) (string, error) {
	var release, images []string
	versions := map[string]bool{}
	for _, tag := range tags {
		if m := releaseTagPattern.FindStringSubmatch(tag); m != nil {
			release = append(release, tag)
			versions[m[1]] = true
		}
		if _, _, ok := imageTag(tag); ok {
			images = append(images, tag)
		}
	}
	if len(release) == 0 && len(images) > 1 {
		slices.Sort(images)
		return "", &AmbiguousReleaseError{
			Tags:   images,
			Reason: fmt.Sprintf("commit %s of %s carries the releases of %d images (%s): release wait follows one; wait for each by its tag, devctl release wait %s %s", short(sha), repository, len(images), strings.Join(images, ", "), repository, images[0]),
		}
	}
	if len(release) == 0 && len(images) == 1 {
		return images[0], nil
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
