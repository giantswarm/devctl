package release

import (
	"regexp"
	"sort"
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

{{ range .Components }}- {{ if eq .PreviousVersion "" }}Added {{ .Name }} {{ .Version }}{{ else if eq .Name "kubernetes" }}Kubernetes from v{{ .PreviousVersion }} to [v{{ .Version }}]({{ .Link }}){{ else if eq .Name "flatcar" }}Flatcar from {{ .PreviousVersion }} to [{{ .Version }}]({{ .Link }}){{ else }}{{ .Name }} from v{{ .PreviousVersion }} to v{{ .Version }}{{ end }}
{{ end }}

{{ range .Components }}{{ if or (eq .Name "kubernetes") (eq .Name "flatcar") }}{{ continue }}{{ end }}
### {{ .Name }} {{ if ne .PreviousVersion "" }}[v{{ .PreviousVersion }}...v{{ .Version }}]({{ .Link }}){{ else }}{{ .Version }}{{ end }}

{{ .Changelog }}
{{ end }}

### Apps

{{ range .Apps }}{{ if eq .PreviousVersion "" }}- Added {{ .Name }} {{ .Version }}
{{ end }}{{ end }}

{{ range .Apps }}{{ if ne .PreviousVersion "" }}- {{ .Name }} from {{ .PreviousVersion }} to {{ .Version }}
{{ end }}{{ end }}

{{ range .Apps }}
### {{ .Name }} {{ if ne .PreviousVersion "" }}[v{{ .PreviousVersion }}...v{{ .Version }}]({{ .Link }}){{ else }}{{ .Version }}{{ end }}

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

	result := writer.String()

	// Clean up the output to remove excess blank lines
	result = cleanReleaseNotes(result)
	return result, nil
}

// cleanReleaseNotes removes excess blank lines from the generated release notes
func cleanReleaseNotes(notes string) string {
	multipleNewlines := regexp.MustCompile(`\n{3,}`)
	notes = multipleNewlines.ReplaceAllString(notes, "\n\n")

	compList := regexp.MustCompile(`(- [^\n]+\n)(\n*)(### [^\n]+)`)
	notes = compList.ReplaceAllString(notes, "$1\n\n$3")

	betweenChangelogs := regexp.MustCompile(`(### [^\n]+[\s\S]+?)(\n{2,})(### [^\n]+)`)
	notes = betweenChangelogs.ReplaceAllString(notes, "$1\n\n$3")

	endNewlines := regexp.MustCompile(`\n{2,}$`)
	notes = endNewlines.ReplaceAllString(notes, "\n")

	notes = regexp.MustCompile(`### Components\n{2,}`).ReplaceAllString(notes, "### Components\n\n")
	notes = regexp.MustCompile(`### Apps\n{2,}`).ReplaceAllString(notes, "### Apps\n\n")

	bulletPoints := regexp.MustCompile(`(- [^\n]+)\n{2,}(- [^\n]+)`)
	notes = bulletPoints.ReplaceAllString(notes, "$1\n$2")

	return notes
}
