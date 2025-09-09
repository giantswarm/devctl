package release

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blang/semver"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

// Calculate the directory name of the given release
func releaseToDirectory(release v1alpha1.Release) string {
	releaseName := strings.Split(release.Name, "-")
	if strings.Contains(release.Name, "cloud-director") {
		return "v" + releaseName[2]
	}
	return "v" + releaseName[1]
}

// Given a slice of versions as strings, return them in ascending semver order with v prefix.
func deduplicateAndSortVersions(originalVersions []string) ([]string, error) {
	versions := map[string]semver.Version{}
	for _, v := range originalVersions {
		parsed, err := semver.Parse(strings.TrimPrefix(v, "v"))
		if err != nil {
			return nil, microerror.Mask(err)
		}
		versions[parsed.String()] = parsed
	}

	var vs []semver.Version
	for _, v := range versions {
		vs = append(vs, v)
	}

	sort.SliceStable(vs, func(i, j int) bool {
		return vs[i].LT(vs[j])
	})

	var result []string
	for _, i := range vs {
		result = append(result, "v"+i.String())
	}
	return result, nil
}

// Return base release with all components and apps from override merged into it.
func mergeReleases(base, override v1alpha1.Release) v1alpha1.Release {
	merged := base
	merged.Name = override.Name
	merged.Spec.State = override.Spec.State
	merged.Spec.Date = override.Spec.Date

	// Where the component exists in both, set version to that of override component.
	for i, component := range merged.Spec.Components {
		for _, overrideComponent := range override.Spec.Components {
			if component.Name == overrideComponent.Name {
				merged.Spec.Components[i].Version = overrideComponent.Version
				break
			}
		}
	}

	// Where the app exists in both, set version to that of override app.
	for i, app := range merged.Spec.Apps {
		for _, overrideApp := range override.Spec.Apps {
			if app.Name == overrideApp.Name {
				merged.Spec.Apps[i].Version = overrideApp.Version
				merged.Spec.Apps[i].ComponentVersion = overrideApp.ComponentVersion
				merged.Spec.Apps[i].DependsOn = overrideApp.DependsOn
				break
			}
		}
	}

	// Where the component doesn't exist in the base, add it directly from override.
	for _, overrideComponent := range override.Spec.Components {
		found := false
		for _, component := range merged.Spec.Components {
			if component.Name == overrideComponent.Name {
				found = true
				break
			}
		}
		if !found {
			merged.Spec.Components = append(merged.Spec.Components, overrideComponent)
		}
	}

	// Where the app doesn't exist in the base, add it directly from override.
	for _, overrideApp := range override.Spec.Apps {
		found := false
		for _, app := range merged.Spec.Apps {
			if app.Name == overrideApp.Name {
				found = true
				break
			}
		}
		if !found {
			merged.Spec.Apps = append(merged.Spec.Apps, overrideApp)
		}
	}

	// Sort apps and components alphabetically by name to maintain consistent ordering
	sort.Slice(merged.Spec.Apps, func(i, j int) bool {
		return merged.Spec.Apps[i].Name < merged.Spec.Apps[j].Name
	})
	sort.Slice(merged.Spec.Components, func(i, j int) bool {
		return merged.Spec.Components[i].Name < merged.Spec.Components[j].Name
	})

	return merged
}

// Parse release.yaml for given version from the given provider path in the releases repository.
func findRelease(providerDirectory string, targetVersion semver.Version) (v1alpha1.Release, string, error) {
	fileInfos, err := os.ReadDir(providerDirectory)
	if err != nil {
		return v1alpha1.Release{}, "", microerror.Mask(err)
	}

	var releaseYAMLPath string
	for _, fileInfo := range fileInfos {
		if !fileInfo.IsDir() || fileInfo.Name() == "archived" {
			continue
		}
		releaseVersion, err := semver.Parse(strings.TrimPrefix(fileInfo.Name(), "v"))
		if err != nil {
			continue
		}
		if releaseVersion.Equals(targetVersion) {
			releaseYAMLPath = filepath.Join(providerDirectory, fileInfo.Name(), "release.yaml")
		}
	}

	if releaseYAMLPath == "" {
		return v1alpha1.Release{}, "", releaseNotFoundError
	}

	releaseYAMLPath = filepath.Clean(releaseYAMLPath)
	releaseYAML, err := os.ReadFile(releaseYAMLPath)
	if err != nil {
		return v1alpha1.Release{}, "", microerror.Mask(err)
	}

	var release v1alpha1.Release
	err = yaml.Unmarshal(releaseYAML, &release)
	if err != nil {
		return v1alpha1.Release{}, "", microerror.Mask(err)
	}

	return release, releaseYAMLPath, nil
}

// marshalReleaseYAML creates a custom YAML representation of the release
// with proper field ordering and without unwanted fields like status and creationTimestamp
func marshalReleaseYAML(release v1alpha1.Release) ([]byte, error) {
	var sb strings.Builder

	// API version and kind
	sb.WriteString("apiVersion: release.giantswarm.io/v1alpha1\n")
	sb.WriteString("kind: Release\n")

	// Metadata (without creationTimestamp)
	sb.WriteString("metadata:\n")
	sb.WriteString("  name: " + release.Name + "\n")

	// Spec
	sb.WriteString("spec:\n")

	// Apps section
	if len(release.Spec.Apps) > 0 {
		sb.WriteString("  apps:\n")
		for _, app := range release.Spec.Apps {
			sb.WriteString("  - name: " + app.Name + "\n")

			// Add catalog if present
			if app.Catalog != "" {
				sb.WriteString("    catalog: " + app.Catalog + "\n")
			}

			sb.WriteString("    version: " + app.Version + "\n")

			if app.ComponentVersion != "" {
				sb.WriteString("    componentVersion: " + app.ComponentVersion + "\n")
			}

			// Add dependsOn if present
			if len(app.DependsOn) > 0 {
				sb.WriteString("    dependsOn:\n")
				for _, dep := range app.DependsOn {
					sb.WriteString("    - " + dep + "\n")
				}
			}
		}
	}

	// Components section
	if len(release.Spec.Components) > 0 {
		sb.WriteString("  components:\n")
		for _, component := range release.Spec.Components {
			sb.WriteString("  - name: " + component.Name + "\n")

			// Add catalog if present
			if component.Catalog != "" {
				sb.WriteString("    catalog: " + component.Catalog + "\n")
			}

			sb.WriteString("    version: " + component.Version + "\n")
		}
	}

	// Date and state
	if release.Spec.Date != nil {
		sb.WriteString("  date: \"" + release.Spec.Date.Format("2006-01-02T15:04:05Z") + "\"\n")
	}
	sb.WriteString("  state: " + string(release.Spec.State) + "\n")

	return []byte(sb.String()), nil
}
