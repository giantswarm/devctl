package release

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/apiextensions/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(_ context.Context, _ *cobra.Command, _ []string) error {
	baseVersion := *semver.MustParse(r.flag.Base) // already validated to be a valid semver string
	providerDirectory := filepath.Join(r.flag.Releases, r.flag.Provider)
	base, baseReleasePath, err := findRelease(providerDirectory, baseVersion)
	if err != nil {
		return microerror.Mask(err)
	}

	var override v1alpha1.Release
	for _, componentVersion := range r.flag.Components {
		split := strings.Split(componentVersion, "=")
		override.Spec.Components = append(override.Spec.Components, v1alpha1.ReleaseSpecComponent{
			Name:    split[0],
			Version: split[1],
		})
	}

	merged := mergeReleases(base, override)
	newVersion := *semver.MustParse(r.flag.Name) // already validated to be a valid semver string
	merged.Name = "v" + newVersion.String()
	err = createRelease(providerDirectory, baseReleasePath, merged)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func createDiff(leftPath string, rightPath string) (string, error) {
	cmd := exec.Command("diff", leftPath, rightPath)
	var writer strings.Builder
	cmd.Stdout = &writer
	cmd.Stderr = os.Stdout
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 { // diff exits with 1 when files differ
			return "", microerror.Mask(exitErr)
		}
	} else if err != nil {
		return "", microerror.Mask(err)
	}
	return writer.String(), nil
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

const releaseNotesTemplate = `# :zap: Giant Swarm Release {{ .Name }} for {{ .Provider }} :zap:

{{ .Description }}

## Change details

{{ range .Components }}
### {{ .Name }} [{{ .Version }}]({{ .Link }})

{{ .Changelog }}

{{ end }}
`

func createReleaseNotes() (string, error) {
	templ, err := template.New("release-notes").Parse(releaseNotesTemplate)
	if err != nil {
		return "", microerror.Mask(err)
	}

	changelog, err := getComponentRelease("aws-operator", "8.7.0")
	if err != nil {
		return "", microerror.Mask(err)
	}

	var writer strings.Builder
	data := struct {
		Name        string
		Provider    string
		Description string
		Components  []struct {
			Name      string
			Version   string
			Link      string
			Changelog string
		}
	}{
		Name:        "test",
		Provider:    "AWS",
		Description: "description",
		Components: []struct {
			Name      string
			Version   string
			Link      string
			Changelog string
		}{
			{
				Name:      "aws-operator",
				Version:   "v1.0.0",
				Link:      "https://github.com/giantswarm/aws-operator/tags/v1.0.0",
				Changelog: changelog,
			},
		},
	}
	err = templ.Execute(&writer, data)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return writer.String(), nil
}

func getComponentRelease(component, version string) (string, error) {
	response, err := http.Get(fmt.Sprintf("https://raw.githubusercontent.com/giantswarm/%s/master/CHANGELOG.md", component))
	if err != nil {
		return "", microerror.Mask(err)
	}

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", microerror.Mask(err)
	}

	split := strings.Split(string(body), "\n")
	start := false
	var changes []string
	pattern := fmt.Sprintf("(?m)^## \\[%s\\].*$", strings.Replace(version, ".", "\\.", -1))
	for _, line := range split {
		matched, err := regexp.Match(pattern, []byte(line))
		if err != nil {
			return "", microerror.Mask(err)
		}

		if matched {
			start = true
			continue
		}
		if start && len(line) > 0 {
			if line[:4] == "## [" {
				break
			}
			changes = append(changes, line)
		}
	}

	if !start {
		return "", fmt.Errorf("changelog not found")
	}

	return strings.Join(changes, "\n"), nil
}

type kustomizationFile struct {
	CommonAnnotations map[string]string `yaml:"commonAnnotations"`
	Resources         []string          `yaml:"resources"`
}

func releaseToDirectory(release v1alpha1.Release) string {
	return release.Name
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

func mergeReleases(base v1alpha1.Release, override v1alpha1.Release) v1alpha1.Release {
	merged := base
	for _, component := range merged.Spec.Components {
		for _, overrideComponent := range override.Spec.Components {
			if component.Name == overrideComponent.Name {
				component.Version = overrideComponent.Version
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
	return merged
}

func createRelease(providerDirectory string, baseReleasePath string, release v1alpha1.Release) error {
	releaseDirectory := releaseToDirectory(release)
	releasePath := filepath.Join(providerDirectory, releaseDirectory)
	err := os.RemoveAll(releasePath)
	if err != nil {
		return microerror.Mask(err)
	}

	err = os.Mkdir(releasePath, 0755)
	if err != nil {
		return microerror.Mask(err)
	}

	releaseYAMLPath := filepath.Join(releasePath, "release.yaml")
	releaseYAML, err := yaml.Marshal(release)
	if err != nil {
		return microerror.Mask(err)
	}

	err = ioutil.WriteFile(releaseYAMLPath, releaseYAML, 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	releaseNotesPath := filepath.Join(releasePath, "README.md")
	releaseNotes, err := createReleaseNotes()
	if err != nil {
		return microerror.Mask(err)
	}

	err = ioutil.WriteFile(releaseNotesPath, []byte(releaseNotes), 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	err = addToKustomization(providerDirectory, release)
	if err != nil {
		return microerror.Mask(err)
	}

	diffPath := filepath.Join(releasePath, "release.diff")
	diff, err := createDiff(baseReleasePath, releaseYAMLPath)
	if err != nil {
		return microerror.Mask(err)
	}
	err = ioutil.WriteFile(diffPath, []byte(diff), 0644)
	if err != nil {
		return microerror.Mask(err)
	}

	err = createKustomization(releasePath)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
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
		return v1alpha1.Release{}, "", invalidConfigError
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
