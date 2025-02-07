package release

import (
	"strings"
	"text/template"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/release-operator/v4/api/v1alpha1"

	"github.com/giantswarm/devctl/v7/pkg/release/changelog"
)

const releaseNotesTemplate = `# :zap: Giant Swarm Release {{ .Name }} for {{ .Provider }} :zap:

{{ .Description }}

## Changes compared to {{ .PreviousName }}

### Components

{{ range .Components }}
{{ if eq .PreviousVersion "" }}
* Added {{ .Name }} [{{ .Version }}]({{ .Link }})
{{ else if eq .Name "kubernetes" }}
* {{ .Name }} from v{{ .PreviousVersion }} to [v{{ .Version }}]({{ .Link }})
{{ else }}
* {{ .Name }} from {{ .PreviousVersion }} to [{{ .Version }}]({{ .Link }})
{{ end }}
{{ end }}

{{ range .Components }}
{{ if or (eq .Name "kubernetes") (eq .Name "flatcar") }}
{{continue}}
{{ end }}

{{ .Changelog }}

{{ end }}

### Apps

{{ range .Apps }}
{{ if eq .PreviousVersion "" }}
* Added {{ .Name }} [{{ .Version }}]({{ .Link }})
{{ else }}
* {{ .Name }} from {{ .PreviousVersion }} to [{{ .Version }}]({{ .Link }})
{{ end }}
{{ end }}

{{ range .Apps }}

{{ .Changelog }}

{{ end }}
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
	"cloud-director": "VMWare Cloud Director",
}

func createReleaseNotes(release, baseRelease v1alpha1.Release, provider string) (string, error) {
	templ, err := template.New("release-notes").Parse(releaseNotesTemplate)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var components []releaseNotes
	var apps []releaseNotes
	for _, component := range release.Spec.Components {
		if component.Name == "os-tooling" {
			// Skip os-tooling for now because it's an internal implementation detail for image naming
			continue
		}

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

		apps = append(apps, releaseNotes{
			Name:            app.Name,
			Version:         app.Version,
			PreviousVersion: previousAppVersion,
			Link:            componentChangelog.Link,
			Changelog:       componentChangelog.Content,
		})
	}

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
