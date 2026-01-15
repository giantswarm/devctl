package release

import (
	"sort"
	"strings"
	"text/template"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/releases/sdk/api/v1alpha1"

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

### {{ .Name }} {{ if ne .PreviousVersion "" }}[v{{ .PreviousVersion }}...v{{ .Version }}]({{ .Link }}){{ else }}[v{{ .Version }}]({{ .Link }}){{ end }}

{{ .Changelog }}
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

### {{ .Name }} {{ if ne .PreviousVersion "" }}[v{{ .PreviousVersion }}...v{{ .Version }}]({{ .Link }}){{ else }}[v{{ .Version }}]({{ .Link }}){{ end }}

{{ .Changelog }}
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
	"kvm":            "KVM",
	"vsphere":        "vSphere",
	"cloud-director": "VMware Cloud Director",
}

func createReleaseNotes(release, baseRelease v1alpha1.Release, provider string) (string, error) {
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

		componentChangelog, err := changelog.ParseChangelog(component.Name, component.Version, previousComponentVersion)
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

		componentChangelog, err := changelog.ParseChangelog(app.Name, app.Version, previousAppVersion)
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

	// Sort components and apps alphabetically by name
	sort.Slice(components, func(i, j int) bool {
		return components[i].Name < components[j].Name
	})
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
