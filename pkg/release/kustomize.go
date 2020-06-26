package release

import (
	"io/ioutil"
	"path/filepath"

	"github.com/giantswarm/apiextensions/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/microerror"
	"sigs.k8s.io/yaml"
)

type kustomizationFile struct {
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	Resources         []string          `json:"resources"`
}

func createKustomization(releaseDirectory string) error {
	content := `resources:
- release.yaml
`
	err := ioutil.WriteFile(filepath.Join(releaseDirectory, "kustomization.yaml"), []byte(content), 0644)
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}

func addToKustomization(providerDirectory string, release v1alpha1.Release) error {
	path := filepath.Join(providerDirectory, "kustomization.yaml")
	var providerKustomization kustomizationFile
	providerKustomizationData, err := ioutil.ReadFile(path)
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

	err = ioutil.WriteFile(path, data, 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func removeFromKustomization(providerDirectory string, release v1alpha1.Release) error {
	path := filepath.Join(providerDirectory, "kustomization.yaml")
	var providerKustomization kustomizationFile
	providerKustomizationData, err := ioutil.ReadFile(path)
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

	err = ioutil.WriteFile(path, data, 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}
