package release

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/releaseclient/pkg/filesystem"
)

// Archives a release on the filesystem from the given parameters. This is the entry point
// for the `devctl archive release` command logic.
func ArchiveRelease(name, releases, provider string) error {
	// Paths
	providerDirectory := filepath.Join(releases, provider)
	fs := filesystem.New(releases)
	release, err := fs.FindRelease(provider, name, false)
	if err != nil {
		return microerror.Mask(err)
	}
	oldPath := filepath.Join(providerDirectory, releaseToDirectory(release))
	newPath := filepath.Join(providerDirectory, "archived", releaseToDirectory(release))

	// Moving the release directory
	err = os.Rename(oldPath, newPath)
	if err != nil {
		return microerror.Mask(err)
	}

	// Editing provider kustomization.yaml
	err = removeFromKustomization(providerDirectory, release)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
