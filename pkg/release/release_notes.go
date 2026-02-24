package release

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/template"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/releases/sdk/api/v1alpha1"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v7/pkg/release/changelog"
)

const releaseNotesTemplate = `# :zap: Giant Swarm Release {{ .Name }} for {{ .Provider }} :zap:

{{ .Description }}

## Changes compared to {{ .PreviousName }}
{{ if .Components }}
### Components
{{ range .Components }}
{{- if eq .PreviousVersion "" }}
- Added {{ .Name }} v{{ .Version }}
{{- else if eq .Name "kubernetes" }}
- Kubernetes from v{{ .PreviousVersion }} to [v{{ .Version }}]({{ .Link }})
{{- else if eq .Name "flatcar" }}
- Flatcar from v{{ .PreviousVersion }} to [v{{ .Version }}]({{ .Link }})
{{- else }}
- {{ .Name }} from v{{ .PreviousVersion }} to v{{ .Version }}
{{- end }}
{{- end }}
{{- range .Components }}
{{- if and (not (eq .Name "kubernetes")) (not (eq .Name "flatcar")) (not (eq .Name "os-tooling")) }}
{{- if .Changelog }}

### {{ .Name }} {{ if ne .PreviousVersion "" }}[v{{ .PreviousVersion }}...v{{ .Version }}]({{ .Link }}){{ else }}[v{{ .Version }}]({{ .Link }}){{ end }}

{{ .Changelog }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{ if .Apps }}
### Apps
{{ range .Apps }}
{{- if eq .PreviousVersion "" }}
- Added {{ .Name }} v{{ .Version }}
{{- else }}
- {{ .Name }} from v{{ .PreviousVersion }} to v{{ .Version }}
{{- end }}
{{- end }}
{{- range .Apps }}
{{- if .Changelog }}

### {{ .Name }} {{ if ne .PreviousVersion "" }}[v{{ .PreviousVersion }}...v{{ .Version }}]({{ .Link }}){{ else }}[v{{ .Version }}]({{ .Link }}){{ end }}

{{ .Changelog }}
{{- end }}
{{- end }}
{{- end }}
`

type releaseNotes struct {
	Name            string
	PreviousVersion string
	Version         string
	Link            string
	Changelog       string
}

type releaseNotesTemplateData struct {
	Name         string
	PreviousName string
	Provider     string
	Description  string
	Components   []releaseNotes
	Apps         []releaseNotes
}

var providerTitleMap = map[string]string{
	"aws":            "CAPA",
	"azure":          "Azure",
	"eks":            "EKS",
	"kvm":            "KVM",
	"vsphere":        "vSphere",
	"cloud-director": "VMware Cloud Director",
}

func createReleaseNotes(release, baseRelease v1alpha1.Release, provider string, changelogNoisePatterns []string) (string, error) {
	templ, err := template.New("release-notes").Parse(releaseNotesTemplate)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var components []releaseNotes
	var apps []releaseNotes
	for _, component := range release.Spec.Components {
		previousComponentVersion := ""
		for _, baseComponent := range baseRelease.Spec.Components {
			if component.Name == baseComponent.Name {
				previousComponentVersion = baseComponent.Version
				break
			}
		}

		if previousComponentVersion == component.Version {
			// Skip components that haven't changed
			continue
		}

		componentChangelog, err := changelog.ParseChangelog(component.Name, component.Version, previousComponentVersion, changelogNoisePatterns...)
		if err != nil {
			return "", microerror.Mask(err)
		}
		if componentChangelog == nil {
			continue
		}

		components = append(components, releaseNotes{
			Name:            component.Name,
			Version:         component.Version,
			PreviousVersion: previousComponentVersion,
			Link:            componentChangelog.Link,
			Changelog:       componentChangelog.Content,
		})
	}

	// Include cluster chart changelog when a provider chart bumps its cluster dependency
	providerCharts := map[string]bool{
		"cluster-aws":            true,
		"cluster-azure":          true,
		"cluster-vsphere":        true,
		"cluster-cloud-director": true,
		"cluster-eks":            true,
	}
	for _, component := range release.Spec.Components {
		if !providerCharts[component.Name] {
			continue
		}
		previousVersion := ""
		for _, base := range baseRelease.Spec.Components {
			if component.Name == base.Name {
				previousVersion = base.Version
				break
			}
		}
		if previousVersion == "" || previousVersion == component.Version {
			continue
		}

		currentClusterVer, err := getClusterDependencyVersion(component.Name, component.Version)
		if err != nil {
			logrus.Warnf("Could not get cluster dependency version from %s v%s: %v", component.Name, component.Version, err)
			continue
		}
		previousClusterVer, err := getClusterDependencyVersion(component.Name, previousVersion)
		if err != nil {
			logrus.Warnf("Could not get cluster dependency version from %s v%s: %v", component.Name, previousVersion, err)
			continue
		}

		if currentClusterVer != "" && previousClusterVer != "" && currentClusterVer != previousClusterVer {
			clusterChangelog, err := changelog.ParseChangelog("cluster", currentClusterVer, previousClusterVer, changelogNoisePatterns...)
			if err != nil {
				logrus.Warnf("Could not parse cluster changelog for %s...%s: %v", previousClusterVer, currentClusterVer, err)
				continue
			}
			if clusterChangelog != nil {
				components = append(components, releaseNotes{
					Name:            "cluster",
					Version:         currentClusterVer,
					PreviousVersion: previousClusterVer,
					Link:            clusterChangelog.Link,
					Changelog:       clusterChangelog.Content,
				})
			}
		}
	}

	for _, app := range release.Spec.Apps {
		previousAppVersion := ""
		for _, baseApp := range baseRelease.Spec.Apps {
			if app.Name == baseApp.Name {
				previousAppVersion = baseApp.Version
				break
			}
		}

		if previousAppVersion == app.Version {
			// Skip apps that haven't changed
			continue
		}

		componentChangelog, err := changelog.ParseChangelog(app.Name, app.Version, previousAppVersion, changelogNoisePatterns...)
		if err != nil {
			return "", microerror.Mask(err)
		}
		if componentChangelog == nil {
			continue
		}

		apps = append(apps, releaseNotes{
			Name:            app.Name,
			Version:         app.Version,
			PreviousVersion: previousAppVersion,
			Link:            componentChangelog.Link,
			Changelog:       componentChangelog.Content,
		})
	}

	// Sort components and apps alphabetically by name,
	// but place "cluster" right after its parent provider chart.
	sort.Slice(components, func(i, j int) bool {
		return components[i].Name < components[j].Name
	})
	// Move "cluster" entry to right after its provider chart
	clusterIdx := -1
	providerIdx := -1
	for i, c := range components {
		if c.Name == "cluster" {
			clusterIdx = i
		}
		if providerCharts[c.Name] {
			providerIdx = i
		}
	}
	if clusterIdx >= 0 && providerIdx >= 0 && clusterIdx != providerIdx+1 {
		entry := components[clusterIdx]
		components = append(components[:clusterIdx], components[clusterIdx+1:]...)
		// Recalculate providerIdx after removal
		for i, c := range components {
			if providerCharts[c.Name] {
				providerIdx = i
				break
			}
		}
		insertAt := providerIdx + 1
		components = append(components[:insertAt], append([]releaseNotes{entry}, components[insertAt:]...)...)
	}
	sort.Slice(apps, func(i, j int) bool {
		return apps[i].Name < apps[j].Name
	})

	var writer strings.Builder
	data := releaseNotesTemplateData{
		Name:         releaseToDirectory(release),
		PreviousName: releaseToDirectory(baseRelease),
		Provider:     providerTitleMap[provider],
		Description:  "<< Add description here >>",
		Components:   components,
		Apps:         apps,
	}
	err = templ.Execute(&writer, data)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return writer.String(), nil
}

// getClusterDependencyVersion fetches the Chart.yaml from a provider chart repo
// at the given version tag and extracts the cluster dependency version.
func getClusterDependencyVersion(providerChartName, version string) (string, error) {
	ref := version
	if !strings.HasPrefix(ref, "v") {
		ref = "v" + ref
	}

	url := fmt.Sprintf("https://raw.githubusercontent.com/giantswarm/%s/%s/helm/%s/Chart.yaml", providerChartName, ref, providerChartName)
	resp, err := http.Get(url)
	if err != nil {
		return "", microerror.Mask(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", microerror.Mask(fmt.Errorf("failed to fetch Chart.yaml from %s: HTTP %d", url, resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", microerror.Mask(err)
	}

	type helmDependency struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	type helmChart struct {
		Dependencies []helmDependency `json:"dependencies"`
	}

	var chart helmChart
	if err := yaml.Unmarshal(body, &chart); err != nil {
		return "", microerror.Mask(err)
	}

	for _, dep := range chart.Dependencies {
		if dep.Name == "cluster" {
			return strings.TrimPrefix(dep.Version, "v"), nil
		}
	}

	return "", nil
}
