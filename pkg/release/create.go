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

	"github.com/blang/semver"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	"github.com/mohae/deepcopy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

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
func CreateRelease(name, base, releases, provider string, components, apps []string, overwrite bool, creationCommand string, bumpall bool, appsToDrop []string, yes bool, output string, verbose bool, changesOnly bool, requestedOnly bool, updateExisting bool) error {
	if updateExisting {
		base = name
	}

	// Determine release type from base and new versions.
	releaseType := ""
	{
		baseV, err := semver.Parse(strings.TrimPrefix(base, "v"))
		if err != nil {
			return microerror.Mask(err)
		}
		newV, err := semver.Parse(strings.TrimPrefix(name, "v"))
		if err != nil {
			return microerror.Mask(err)
		}

		if newV.Major > baseV.Major {
			releaseType = "major"
		} else if newV.Minor > baseV.Minor {
			releaseType = "minor"
		} else {
			releaseType = "patch"
		}

		if updateExisting && releaseType == "patch" {
			releaseType = "minor"
		}
	}

	// Paths
	baseVersion, err := semver.Parse(strings.TrimPrefix(base, "v"))
	if err != nil {
		return microerror.Mask(err)
	}
	providerDirectory := ""
	if provider == "aws" {
		// TODO: Directory for AWS provider is currently 'capa' because of old vintage releases located in aws directory
		// This will change in the future
		providerDirectory = filepath.Join(releases, "capa")
	} else {
		providerDirectory = filepath.Join(releases, provider)
	}

	requests, err := readRequests(providerDirectory, name)
	if err != nil {
		return microerror.Mask(err)
	}

	// Find the base release
	baseRelease, baseReleasePath, err := findRelease(providerDirectory, baseVersion)
	if err != nil {
		return microerror.Mask(err)
	}

	// When using --update-existing with specific component/app updates, use existing release as base
	// to preserve all previous modifications
	var effectiveBaseRelease v1alpha1.Release
	if updateExisting && (len(components) > 0 || len(apps) > 0) && !bumpall {
		// Try to read the existing release in the current branch
		newVersion, err := semver.Parse(strings.TrimPrefix(name, "v"))
		if err != nil {
			return microerror.Mask(err)
		}
		existingRelease, _, err := findRelease(providerDirectory, newVersion)
		if err == nil {
			// Use the existing release as base to preserve previous modifications
			effectiveBaseRelease = existingRelease
			if verbose {
				fmt.Printf("Using existing release %s as base to preserve previous modifications\n", name)
			}
		} else {
			// No existing release found, use the original base
			effectiveBaseRelease = baseRelease
			if verbose {
				fmt.Printf("No existing release found for %s, using base release\n", name)
			}
		}
	} else {
		effectiveBaseRelease = baseRelease
	}

	// Store the base release for later use because it gets modified
	previousRelease := deepcopy.Copy(baseRelease).(v1alpha1.Release)

	// Determine which apps to drop based on the new release version.
	appsToDropForThisRelease := make(map[string]bool)
	releaseVersion, err := semver.Parse(strings.TrimPrefix(name, "v"))
	if err == nil {
		for _, appToDrop := range appsToBeDropped {
			if releaseVersion.Major >= appToDrop.MajorVersion {
				appsToDropForThisRelease[appToDrop.Name] = true
			}
		}
	}

	// Auto-detect components that are not explicitly provided by the user.
	if !requestedOnly && releaseType != "patch" {
		for componentName, params := range changelog.KnownComponents {
			if !params.AutoDetect {
				continue
			}

			// This is now handled in BumpAll.
			if componentName == "kubernetes" {
				continue
			}

			// Check if the component is in the base release.
			inBaseRelease := false
			for _, component := range effectiveBaseRelease.Spec.Components {
				if component.Name == componentName {
					inBaseRelease = true
					break
				}
			}
			for _, app := range effectiveBaseRelease.Spec.Apps {
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
					if verbose {
						fmt.Printf("Explicit component specified by user: %s\n", componentVersion)
					}
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
					if verbose {
						fmt.Printf("Explicit app specified by user: %s\n", appVersion)
					}
					isProvidedByUser = true
					break
				}
			}
			if isProvidedByUser {
				continue
			}

			// Attempt to auto-detect the component version.
			if verbose {
				fmt.Printf("No explicit %s component specified by user. Attempting auto-detection based on release name pattern...\n", componentName)
			}
			var detectedVersion string
			var err error
			detectedVersion, err = autoDetectVersion(name, componentName)

			if err != nil {
				fmt.Printf("Warning: Could not auto-detect %s version: %v\n", componentName, err)
				fmt.Printf("You can manually specify the version using --component %s@<version> or --app %s@<version>\n", componentName, componentName)
			} else {
				app := fmt.Sprintf("%s@%s", componentName, detectedVersion)
				apps = append(apps, app)
				if verbose {
					fmt.Printf("Auto-detected and added app: %s\n", app)
				}
			}
		}
	}

	if bumpall {
		if verbose {
			fmt.Println("Requested automated bumping of all components and apps.")
		}

		if releaseType == "patch" && len(components) == 0 && len(apps) == 0 && output != "markdown" {
			fmt.Println("For patch releases, --bumpall does not automatically bump any component or app.")
			fmt.Println("To bump a specific component or app, please use the --component or --app flags.")
		}

		// Pin k8s version to the release major version.
		releaseVersionForK8s, err := semver.Parse(strings.TrimPrefix(name, "v"))
		if err != nil {
			return microerror.Mask(err)
		}
		major := releaseVersionForK8s.Major

		components, apps, err = BumpAll(effectiveBaseRelease, components, apps, releaseType, appsToDropForThisRelease, requests, yes, output, changesOnly, requestedOnly, major)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	// Define release CR
	var updatesRelease v1alpha1.Release
	newVersion, err := semver.Parse(strings.TrimPrefix(name, "v"))
	if err != nil {
		return microerror.Mask(err)
	}
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
			fmt.Println("App must be specified as <name>@<version>[@<component_version>][@<dependency>[#<another-dependency]>], got", appVersion)
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
			dependencies = strings.Split(split[3], "#")
		}

		updatesRelease.Spec.Apps = append(updatesRelease.Spec.Apps, v1alpha1.ReleaseSpecApp{
			Name:             name,
			Version:          version,
			ComponentVersion: componentVersion,
			DependsOn:        dependencies,
		})

	}
	newRelease := mergeReleases(effectiveBaseRelease, updatesRelease)

	// Drop apps that are no longer supported in this release.
	if len(appsToDropForThisRelease) > 0 {
		var filteredMergedApps []v1alpha1.ReleaseSpecApp
		for _, app := range newRelease.Spec.Apps {
			if _, shouldDrop := appsToDropForThisRelease[app.Name]; shouldDrop {
				if verbose {
					fmt.Printf("Dropping %s from release %s as it is no longer supported.\n", app.Name, name)
				}
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
	// For update-existing, we need to find the actual previous version for a meaningful diff
	var diffBaseReleasePath string
	if updateExisting {
		// Find the previous version to diff against
		previousVersion, err := findPreviousReleaseVersion(providerDirectory, newVersion)
		if err == nil {
			_, diffBaseReleasePath, err = findRelease(providerDirectory, previousVersion)
			if err != nil {
				// Fall back to using the base release path
				diffBaseReleasePath = baseReleasePath
			}
		} else {
			// Fall back to using the base release path
			diffBaseReleasePath = baseReleasePath
		}
	} else {
		diffBaseReleasePath = baseReleasePath
	}

	diff, err := createDiff(diffBaseReleasePath, releaseYAMLPath)
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
		vi, err := semver.Parse(strings.TrimPrefix(releasesJson.Releases[i].Version, "v"))
		if err != nil {
			return false
		}
		vj, err := semver.Parse(strings.TrimPrefix(releasesJson.Releases[j].Version, "v"))
		if err != nil {
			return false
		}
		return vi.LT(vj)
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

func readRequests(providerDirectory, version string) ([]Request, error) {
	requestsYAMLPath := filepath.Join(providerDirectory, "requests.yaml")
	data, err := os.ReadFile(requestsYAMLPath)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, microerror.Mask(err)
	}

	var requests Requests
	err = yaml.Unmarshal(data, &requests)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	version = strings.TrimPrefix(version, "v")
	return requests.ForVersion(version)
}
