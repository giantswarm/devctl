package release

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	"github.com/mohae/deepcopy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// CreateRelease creates a release on the filesystem from the given parameters. This is the entry point
// for the `devctl create release` command logic.
func CreateRelease(name, base, releases, provider string, components, apps []string, overwrite bool, creationCommand string, bumpall bool) error {
	// Paths
	baseVersion := *semver.MustParse(base) // already validated to be a valid semver string
	providerDirectory := ""
	if provider == "aws" {
		// TODO: Directory for AWS provider is currently 'capa' because of old vintage releases located in aws directory
		// This will change in the future
		providerDirectory = filepath.Join(releases, "capa")
	} else {
		providerDirectory = filepath.Join(releases, provider)
	}

	baseRelease, baseReleasePath, err := findRelease(providerDirectory, baseVersion)
	if err != nil {
		return microerror.Mask(err)
	}

	// Store the base release for later use because it gets modified
	previousRelease := deepcopy.Copy(baseRelease).(v1alpha1.Release)

	if bumpall {
		fmt.Println("Requested automated bumping of all components and apps.")
		components, apps, err = BumpAll(baseRelease, components, apps)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// Define release CR
	var updatesRelease v1alpha1.Release
	newVersion := *semver.MustParse(name) // already validated to be a valid semver string
	updatesRelease.Name = fmt.Sprintf("%s-%s", provider, newVersion.String())
	now := metav1.Now()
	updatesRelease.Spec.Date = &now
	updatesRelease.Spec.State = "active"
	for _, componentVersion := range components {
		split := strings.Split(componentVersion, "@")
		if len(split) != 2 {
			fmt.Println("Component must be specified as <name>@<version>, got", componentVersion)
			return microerror.Mask(badFormatError)
		}
		updatesRelease.Spec.Components = append(updatesRelease.Spec.Components, v1alpha1.ReleaseSpecComponent{
			Name:    split[0],
			Version: split[1],
		})
	}
	for _, appVersion := range apps {
		split := strings.Split(appVersion, "@")
		if len(split) != 2 && len(split) != 3 {
			fmt.Println("App must be specified as <name>@<version>, got", appVersion)
			return microerror.Mask(badFormatError)
		}
		name := split[0]
		version := split[1]
		var componentVersion string
		if len(split) > 2 {
			componentVersion = split[2]
		}
		updatesRelease.Spec.Apps = append(updatesRelease.Spec.Apps, v1alpha1.ReleaseSpecApp{
			Name:             name,
			Version:          version,
			ComponentVersion: componentVersion,
		})
	}
	newRelease := mergeReleases(baseRelease, updatesRelease)
	releaseDirectory := releaseToDirectory(newRelease)
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

	// Release CR
	releaseYAMLPath := filepath.Join(releasePath, "release.yaml")
	releaseYAML, err := yaml.Marshal(newRelease)
	if err != nil {
		return microerror.Mask(err)
	}
	// Prepend command used for creation
	{
		yamlComment := []byte(fmt.Sprintf("# Generated with:\n# %s\n", creationCommand))
		releaseYAML = append(yamlComment, releaseYAML...)
	}
	err = os.WriteFile(releaseYAMLPath, releaseYAML, 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}

	// Release notes
	releaseNotesPath := filepath.Join(releasePath, "README.md")
	releaseNotes, err := createReleaseNotes(updatesRelease, previousRelease, provider)
	if err != nil {
		return microerror.Mask(err)
	}
	err = os.WriteFile(releaseNotesPath, []byte(releaseNotes), 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}

	// Release diff
	diffPath := filepath.Join(releasePath, "release.diff")
	diff, err := createDiff(baseReleasePath, releaseYAMLPath)
	if err != nil {
		return microerror.Mask(err)
	}
	err = os.WriteFile(diffPath, []byte(diff), 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}

	// Release announcement.md
	announcementPath := filepath.Join(releasePath, "announcement.md")
	announcement, err := createAnnouncement(updatesRelease, provider)
	if err != nil {
		return microerror.Mask(err)
	}
	err = os.WriteFile(announcementPath, []byte(announcement), 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
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
