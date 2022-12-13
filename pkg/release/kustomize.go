package release

import (
	"os"
	"path/filepath"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v3/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

type kustomizationFile struct {
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	Transformers      []string          `json:"transformers"`
	Resources         []string          `json:"resources"`
}

// Create a release kustomization.yaml which simply defines the release.yaml as a resource.
func createKustomization(releaseDirectory string) error {
	content := `resources:
- release.yaml
`
	err := os.WriteFile(filepath.Join(releaseDirectory, "kustomization.yaml"), []byte(content), 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}

// Add the given release to the provider kustomization.yaml, sorting and de-duplicating resources as needed.
func addToKustomization(providerDirectory string, release v1alpha1.Release) error {
	path := filepath.Join(providerDirectory, "kustomization.yaml")
	var providerKustomization kustomizationFile
	providerKustomizationData, err := os.ReadFile(path)
	if err != nil {
		return microerror.Mask(err)
	}

	err = yaml.UnmarshalStrict(providerKustomizationData, &providerKustomization)
	if err != nil {
		return microerror.Mask(err)
	}

	providerKustomization.Resources = append(providerKustomization.Resources, releaseToDirectory(release))
	providerKustomization.Resources, err = deduplicateAndSortVersions(providerKustomization.Resources)
	if err != nil {
		return microerror.Mask(err)
	}

	data, err := yaml.Marshal(providerKustomization)
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.WriteFile(path, data, 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// Remove the given release from the provider kustomization.yaml.
func removeFromKustomization(providerDirectory string, release v1alpha1.Release) error {
	path := filepath.Join(providerDirectory, "kustomization.yaml")
	var providerKustomization kustomizationFile
	providerKustomizationData, err := os.ReadFile(path)
	if err != nil {
		return microerror.Mask(err)
	}

	err = yaml.UnmarshalStrict(providerKustomizationData, &providerKustomization)
	if err != nil {
		return microerror.Mask(err)
	}

	for i, r := range providerKustomization.Resources {
		if r == releaseToDirectory(release) {
			providerKustomization.Resources = append(providerKustomization.Resources[:i], providerKustomization.Resources[i+1:]...)
			break
		}
	}
	providerKustomization.Resources, err = deduplicateAndSortVersions(providerKustomization.Resources)
	if err != nil {
		return microerror.Mask(err)
	}

	data, err := yaml.Marshal(providerKustomization)
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.WriteFile(path, data, 0644) //nolint:gosec
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
