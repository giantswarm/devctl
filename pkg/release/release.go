package release

import (
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/apiextensions/v2/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/microerror"
)

// Calculate the directory name of the given release
func releaseToDirectory(release v1alpha1.Release) string {
	return release.Name
}

// Given a slice of versions as strings, return them in ascending semver order with v prefix.
func deduplicateAndSortVersions(originalVersions []string) ([]string, error) {
	versions := map[string]*semver.Version{}
	for _, v := range originalVersions {
		parsed, err := semver.NewVersion(v)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		versions[parsed.String()] = parsed
	}

	var vs []*semver.Version
	for _, v := range versions {
		vs = append(vs, v)
	}

	sort.Sort(semver.Collection(vs))

	var result []string
	for _, i := range vs {
		result = append(result, "v"+i.String())
	}
	return result, nil
}
