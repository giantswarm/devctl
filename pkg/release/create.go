package release

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	"github.com/mohae/deepcopy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/giantswarm/devctl/v7/pkg/release/changelog"
)

type droppedAppConfig struct {
	Name         string
	MajorVersion uint64
}

var appsToBeDropped = []droppedAppConfig{
	{
		Name:         "karpenter-nodepools",
		MajorVersion: 32,
	},
}

// CreateRelease creates a release on the filesystem from the given parameters. This is the entry point
// for the `devctl create release` command logic.
func CreateRelease(name, base, releases, provider string, components, apps []string, overwrite bool, creationCommand string, bumpall, yes bool) error {
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

	// Determine which apps to drop based on the new release version.
	appsToDropForThisRelease := make(map[string]bool)
	releaseVersion, err := semver.NewVersion(strings.TrimPrefix(name, "v"))
	if err == nil {
		for _, appToDrop := range appsToBeDropped {
			if releaseVersion.Major() >= appToDrop.MajorVersion {
				appsToDropForThisRelease[appToDrop.Name] = true
			}
		}
	}

	// Auto-detect components that are not explicitly provided by the user.
	// The auto-detection logic is driven by the `AutoDetect` flag in the `KnownComponents` map.
	// For each component with this flag, we check if it was present in the base release
	// and if the user has not already provided a version for it.
	for componentName, params := range changelog.KnownComponents {
		if !params.AutoDetect {
			continue
		}

		// Check if the component is in the base release.
		inBaseRelease := false
		for _, component := range baseRelease.Spec.Components {
			if component.Name == componentName {
				inBaseRelease = true
				break
			}
		}
		for _, app := range baseRelease.Spec.Apps {
			if app.Name == componentName {
				inBaseRelease = true
				break
			}
		}
		if !inBaseRelease {
			continue
		}

		// Check if the user has already provided the component.
		isProvidedByUser := false
		for _, componentVersion := range components {
			split := strings.Split(componentVersion, "@")
			if len(split) >= 1 && split[0] == componentName {
				fmt.Printf("Explicit component specified by user: %s\n", componentVersion)
				isProvidedByUser = true
				break
			}
		}
		if isProvidedByUser {
			continue
		}
		for _, appVersion := range apps {
			split := strings.Split(appVersion, "@")
			if len(split) >= 1 && split[0] == componentName {
				fmt.Printf("Explicit app specified by user: %s\n", appVersion)
				isProvidedByUser = true
				break
			}
		}
		if isProvidedByUser {
			continue
		}

		// Attempt to auto-detect the component version.
		fmt.Printf("No explicit %s component specified by user. Attempting auto-detection based on release name pattern...\n", componentName)
		var detectedVersion string
		var err error
		detectedVersion, err = autoDetectVersion(name, componentName)

		if err != nil {
			fmt.Printf("Warning: Could not auto-detect %s version: %v\n", componentName, err)
			fmt.Printf("You can manually specify the version using --component %s@<version> or --app %s@<version>\n", componentName, componentName)
		} else {
			if componentName == "kubernetes" {
				component := fmt.Sprintf("%s@%s", componentName, detectedVersion)
				components = append(components, component)
				fmt.Printf("Auto-detected and added component: %s\n", component)
			} else {
				app := fmt.Sprintf("%s@%s", componentName, detectedVersion)
				apps = append(apps, app)
				fmt.Printf("Auto-detected and added app: %s\n", app)
			}
		}
	}

	if bumpall {
		fmt.Println("Requested automated bumping of all components and apps.")
		components, apps, err = BumpAll(baseRelease, components, apps, appsToDropForThisRelease, yes)
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
		if len(split) < 2 || len(split) > 4 {
			fmt.Println("App must be specified as <name>@<version>[@<component_version>][@<dependencies>], got", appVersion)
			return microerror.Mask(badFormatError)
		}
		name := split[0]
		version := split[1]

		var componentVersion string
		if len(split) > 2 {
			componentVersion = split[2]
		}

		var dependencies []string
		if len(split) > 3 && split[3] != "" {
			dependencies = strings.Split(split[3], ",")
		}

		updatesRelease.Spec.Apps = append(updatesRelease.Spec.Apps, v1alpha1.ReleaseSpecApp{
			Name:             name,
			Version:          version,
			ComponentVersion: componentVersion,
			DependsOn:        dependencies,
		})

	}
	newRelease := mergeReleases(baseRelease, updatesRelease)

	// Drop apps that are no longer supported in this release.
	if len(appsToDropForThisRelease) > 0 {
		var filteredMergedApps []v1alpha1.ReleaseSpecApp
		for _, app := range newRelease.Spec.Apps {
			if _, shouldDrop := appsToDropForThisRelease[app.Name]; shouldDrop {
				fmt.Printf("Dropping %s from release %s as it is no longer supported.\n", app.Name, name)
				continue
			}
			filteredMergedApps = append(filteredMergedApps, app)
		}
		newRelease.Spec.Apps = filteredMergedApps
	}

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
	err = os.Mkdir(releasePath, 0750)
	if err != nil {
		return microerror.Mask(err)
	}

	// Release CR
	releaseYAMLPath := filepath.Join(releasePath, "release.yaml")
	releaseYAML, err := marshalReleaseYAML(newRelease)
	if err != nil {
		return microerror.Mask(err)
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
	err = createKustomization(releasePath, provider)
	if err != nil {
		return microerror.Mask(err)
	}

	// Provider kustomization.yaml
	err = addToKustomization(providerDirectory, newRelease)
	if err != nil {
		return microerror.Mask(err)
	}

	// Update releases.json
	releasesJSONPath := filepath.Join(providerDirectory, "releases.json")
	releasesJSONPath = filepath.Clean(releasesJSONPath)
	releasesData, err := os.ReadFile(releasesJSONPath)
	if err != nil {
		return microerror.Mask(err)
	}

	var releasesJson ReleasesJsonData
	err = json.Unmarshal(releasesData, &releasesJson)
	if err != nil {
		return microerror.Mask(err)
	}

	if provider == "aws" {
		provider = "capa"
	}

	newReleaseInfo := ReleaseJsonInfo{
		Version:          newVersion.String(),
		IsDeprecated:     false,
		ReleaseTimestamp: now.Format(time.RFC3339),
		ChangelogUrl:     fmt.Sprintf("https://github.com/giantswarm/releases/blob/master/%s/%s/README.md", provider, releaseDirectory),
		IsStable:         true,
	}

	// Replace if it's already there
	releasesJson.Releases = slices.DeleteFunc(releasesJson.Releases, func(releaseJson ReleaseJsonInfo) bool {
		return releaseJson.Version == newReleaseInfo.Version
	})

	releasesJson.Releases = append(releasesJson.Releases, newReleaseInfo)

	// sort releases in json by version
	sort.SliceStable(releasesJson.Releases, func(i, j int) bool {
		vi := semver.MustParse(releasesJson.Releases[i].Version)
		vj := semver.MustParse(releasesJson.Releases[j].Version)
		return vi.LessThan(vj)
	})

	updatedReleasesData, err := json.MarshalIndent(releasesJson, "", "  ")
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.WriteFile(releasesJSONPath, updatedReleasesData, 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
