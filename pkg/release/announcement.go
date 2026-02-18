package release

import (
	"strings"
	"text/template"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/releases/sdk/api/v1alpha1"
)

const announcementNotesTemplate = `**Workload cluster release {{ .Release }} for {{ .Provider }} is available**. {{ .Description }}

Further details can be found in the [release notes](https://docs.giantswarm.io/changes/workload-cluster-releases-{{ .DocProvider }}/releases/{{ .ReleaseDirectory }}).
`

var providerDocMap = map[string]string{
	"aws":            "capa",
	"azure":          "azure",
	"eks":            "eks",
	"vsphere":        "vsphere",
	"cloud-director": "cloud-director",
}

type announcementNotesTemplateData struct {
	Release          string
	ReleaseDirectory string
	Provider         string
	DocProvider      string
	Description      string
	Components       []releaseNotes
	Apps             []releaseNotes
}

func createAnnouncement(release v1alpha1.Release, provider string) (string, error) {
	templ, err := template.New("announcement-notes").Parse(announcementNotesTemplate)
	if err != nil {
		return "", microerror.Mask(err)
	}

	var writer strings.Builder
	data := announcementNotesTemplateData{
		Release:          releaseToDirectory(release),
		ReleaseDirectory: release.Name,
		Provider:         providerTitleMap[provider],
		DocProvider:      providerDocMap[provider],
		Description:      "<< Add description here >>",
	}
	err = templ.Execute(&writer, data)
	if err != nil {
		return "", microerror.Mask(err)
	}

	return writer.String(), nil
}
