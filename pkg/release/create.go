package release

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/releases/pkg/filesystem"
	"github.com/giantswarm/releases/pkg/patch"
	"sigs.k8s.io/yaml"
)

// Creates a release on the filesystem from the given parameters. This is the entry point
// for the `devctl create release` command logic.
func CreateRelease(patchFile, base, releases, provider string, overwrite bool) error {
	releasePatchContent, err := ioutil.ReadFile(patchFile)
	if err != nil {
		return microerror.Mask(err)
	}
	var releasePatch patch.ReleasePatch
	err = yaml.UnmarshalStrict(releasePatchContent, &releasePatch)
	if err != nil {
		return microerror.Mask(err)
	}

	// Paths
	fs := filesystem.New(releases)
	baseRelease, err := fs.FindRelease(provider, base, false)
	if err != nil {
		return microerror.Mask(err)
	}

	updatedBase, newRelease := patch.Apply(baseRelease, releasePatch)
	newRelease.TypeMeta = updatedBase.TypeMeta
	releaseDirectory := releaseToDirectory(newRelease)
	providerDirectory := filepath.Join(releases, provider)
	releasePath := filepath.Join(providerDirectory, releaseDirectory)

	// Delete existing if overwrite
	if overwrite {
		err = os.RemoveAll(releasePath)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// Release directory
	err = os.Mkdir(releasePath, 0755)
	if err != nil {
		return microerror.Mask(err)
	}

	// Write new release CR
	releaseYAMLPath := filepath.Join(releasePath, "release.yaml")
	releaseYAML, err := yaml.Marshal(newRelease)
	if err != nil {
		return microerror.Mask(err)
	}
	err = ioutil.WriteFile(releaseYAMLPath, releaseYAML, 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	// Deprecate base release
	baseReleaseDirectory := releaseToDirectory(baseRelease)
	baseReleasePath := filepath.Join(providerDirectory, baseReleaseDirectory)
	baseYAMLPath := filepath.Join(baseReleasePath, "release.yaml")
	releaseYAML, err = yaml.Marshal(updatedBase)
	if err != nil {
		return microerror.Mask(err)
	}
	err = ioutil.WriteFile(baseYAMLPath, releaseYAML, 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	// Release notes
	releaseNotesPath := filepath.Join(releasePath, "README.md")
	releaseNotes, err := createReleaseNotes(newRelease.Name, releasePatch, provider)
	if err != nil {
		return microerror.Mask(err)
	}
	err = ioutil.WriteFile(releaseNotesPath, []byte(releaseNotes), 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	// Write the patch to the release directory
	diffPath := filepath.Join(releasePath, "patch.yaml")
	if overwrite || diffPath != patchFile {
		err = ioutil.WriteFile(diffPath, releasePatchContent, 0644)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// Release kustomization.yaml
	err = createKustomization(releasePath)
	if err != nil {
		return microerror.Mask(err)
	}

	// Provider kustomization.yaml
	err = addToKustomization(providerDirectory, newRelease)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
