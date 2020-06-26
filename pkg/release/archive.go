package release

import (
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
)

func ArchiveRelease(name, releases, provider string) error {
	// Paths
	version := *semver.MustParse(name) // already validated to be a valid semver string
	providerDirectory := filepath.Join(releases, provider)
	release, _, err := findRelease(providerDirectory, version)
	if err != nil {
		return microerror.Mask(err)
	}

	oldPath := filepath.Join(providerDirectory, releaseToDirectory(release))
	newPath := filepath.Join(providerDirectory, "archived", releaseToDirectory(release))
	err = os.Rename(oldPath, newPath)
	if err != nil {
		return microerror.Mask(err)
	}

	err = removeFromKustomization(providerDirectory, release)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
