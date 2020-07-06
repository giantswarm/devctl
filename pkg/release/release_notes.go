package release

import (
	"strings"
	"text/template"

	"github.com/giantswarm/apiextensions/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/changelog"
)

const releaseNotesTemplate = `# :zap: Giant Swarm Release {{ .Name }} for {{ .Provider }} :zap:

{{ .Description }}

## Change details

{{ range .Components }}
### {{ .Name }} [{{ .Version }}]({{ .Link }})

{{ .Changelog }}

{{ end }}
`

type releaseNotesComponent struct {
	Name      string
	Version   string
	Link      string
	Changelog string
}

type releaseNotesTemplateData struct {
	Name        string
	Provider    string
	Description string
	Components  []releaseNotesComponent
}

var providerTitleMap = map[string]string{
	"aws":   "AWS",
	"azure": "Azure",
	"kvm":   "KVM",
}

func createReleaseNotes(release v1alpha1.Release, provider string) (string, error) {
	templ, err := template.New("release-notes").Parse(releaseNotesTemplate)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var components []releaseNotesComponent
	for _, component := range release.Spec.Components {
		componentChangelog, err := changelog.ParseChangelog(component.Name, component.Version)
		if err != nil {
			return "", microerror.Mask(err)
		}
		components = append(components, releaseNotesComponent{
			Name:      component.Name,
			Version:   component.Version,
			Link:      componentChangelog.Link,
			Changelog: componentChangelog.Content,
		})
	}

	var writer strings.Builder
	data := releaseNotesTemplateData{
		Name:        release.Name,
		Provider:    providerTitleMap[provider],
		Description: "<< Add description here >>",
		Components:  components,
	}
	err = templ.Execute(&writer, data)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return writer.String(), nil
}
