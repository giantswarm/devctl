package release

import (
	"io/ioutil"
	"path/filepath"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/apiextensions/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/yaml"
)

func releaseToDirectory(release v1alpha1.Release) string {
	return release.Name
}

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

func mergeReleases(base v1alpha1.Release, override v1alpha1.Release) v1alpha1.Release {
	merged := base
	merged.Name = override.Name

	for i, component := range merged.Spec.Components {
		for _, overrideComponent := range override.Spec.Components {
			if component.Name == overrideComponent.Name {
				merged.Spec.Components[i].Version = overrideComponent.Version
				break
			}
		}
	}

	for i, app := range merged.Spec.Apps {
		for _, overrideApp := range override.Spec.Apps {
			if app.Name == overrideApp.Name {
				merged.Spec.Apps[i].Version = overrideApp.Version
				break
			}
		}
	}

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

	return merged
}

func findRelease(providerDirectory string, targetVersion semver.Version) (v1alpha1.Release, string, error) {
	fileInfos, err := ioutil.ReadDir(providerDirectory)
	if err != nil {
		return v1alpha1.Release{}, "", microerror.Mask(err)
	}

	var releaseYAMLPath string
	for _, fileInfo := range fileInfos {
		if !fileInfo.IsDir() || fileInfo.Name() == "archived" {
			continue
		}
		releaseVersion, err := semver.NewVersion(fileInfo.Name())
		if err != nil {
			continue
		}
		if releaseVersion.String() == targetVersion.String() {
			releaseYAMLPath = filepath.Join(providerDirectory, fileInfo.Name(), "release.yaml")
		}
	}

	if releaseYAMLPath == "" {
		return v1alpha1.Release{}, "", releaseNotFoundError
	}

	releaseYAML, err := ioutil.ReadFile(releaseYAMLPath)
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
