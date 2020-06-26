package release

import (
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/apiextensions/pkg/apis/release/v1alpha1"
	"github.com/giantswarm/microerror"
)

const releaseNotesTemplate = `# :zap: Giant Swarm Release {{ .Name }} for {{ .Provider }} :zap:

{{ .Description }}

## Change details

{{ range .Components }}
### {{ .Name }} [{{ .Version }}]({{ .Link }})

{{ .Changelog }}

{{ end }}
`

type componentChangelogParams struct {
	tag          string
	changelog    string
	start        string
	intermediate string
	end          string
}

var componentChangelogs = map[string]componentChangelogParams{
	"app-operator": {
		tag:       "https://github.com/giantswarm/app-operator/releases/tag/{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/app-operator/master/CHANGELOG.md",
		start:     "(?m)^## \\[v?{{.VersionEscaped}}\\].*$",
		end:       "(?m)^## .*$",
	},
	"aws-operator": {
		tag:       "https://github.com/giantswarm/aws-operator/releases/tag/{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/aws-operator/master/CHANGELOG.md",
		start:     "(?m)^## \\[v?{{.VersionEscaped}}\\].*$",
		end:       "(?m)^(## .*|\\[Unreleased\\].*)$",
	},
	"cert-operator": {
		tag:       "https://github.com/giantswarm/cert-operator/releases/tag/{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cert-operator/master/CHANGELOG.md",
		start:     "(?m)^## \\[v?{{.VersionEscaped}}\\].*$",
		end:       "(?m)^(## .*|\\[Unreleased\\].*)$",
	},
	"cluster-operator": {
		tag:       "https://github.com/giantswarm/cluster-operator/releases/tag/{{.Version}}",
		changelog: "https://raw.githubusercontent.com/giantswarm/cluster-operator/master/CHANGELOG.md",
		start:     "(?m)^## \\[v?{{.VersionEscaped}}\\].*$",
		end:       "(?m)^## .*$",
	},
	"kubernetes": {
		tag:          "https://github.com/kubernetes/kubernetes/releases/tag/{{.Version}}",
		changelog:    "https://raw.githubusercontent.com/kubernetes/kubernetes/master/CHANGELOG/CHANGELOG-{{.Major}}.{{.Minor}}.md",
		start:        "(?m)^# v{{.VersionEscaped}}$",
		intermediate: "(?m)^## Changes by Kind$",
		end:          "(?m)^# .*$",
	},
	"etcd": {
		tag:       "https://github.com/etcd-io/etcd/releases/tag/{{.Version}}",
		changelog: "https://raw.githubusercontent.com/etcd-io/etcd/master/CHANGELOG-{{.Major}}.{{.Minor}}.md",
		start:     "(?m)^## \\[v?{{.VersionEscaped}}\\].*$",
		end:       "(?m)^## .*$",
	},
	"aws-cni": {
		tag:       "https://github.com/aws/amazon-vpc-cni-k8s/releases/tag/{{.Version}}",
		changelog: "https://raw.githubusercontent.com/aws/amazon-vpc-cni-k8s/master/CHANGELOG.md",
		start:     "(?m)^## v{{.VersionEscaped}}$",
		end:       "(?m)^##? .*$",
	},
}

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
		componentRelease, err := getComponentRelease(component.Name, component.Version)
		if err != nil {
			return "", microerror.Mask(err)
		}
		components = append(components, componentRelease)
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

func getComponentRelease(componentName string, version string) (releaseNotesComponent, error) {
	result := releaseNotesComponent{
		Name:    componentName,
		Version: version,
	}

	templateData := struct {
		Major          uint64
		Minor          uint64
		Version        string
		VersionEscaped string
	}{
		Version:        version,
		VersionEscaped: strings.Replace(version, ".", "\\.", -1),
	}
	parsedVersion, err := semver.NewVersion(version)
	if err == nil {
		templateData.Major = parsedVersion.Major()
		templateData.Minor = parsedVersion.Minor()
	}

	params := componentChangelogs[componentName]

	if params.tag != "" {
		linkTemplate, err := template.New("link").Parse(params.tag)
		if err != nil {
			return releaseNotesComponent{}, err
		}
		var linkBuffer strings.Builder
		err = linkTemplate.Execute(&linkBuffer, templateData)
		if err != nil {
			return releaseNotesComponent{}, err
		}
		result.Link = linkBuffer.String()
	}

	if params.changelog != "" {
		var url string
		{
			urlTemplate, err := template.New("url").Parse(params.changelog)
			if err != nil {
				return releaseNotesComponent{}, microerror.Mask(err)
			}
			var urlBuffer strings.Builder
			err = urlTemplate.Execute(&urlBuffer, templateData)
			if err != nil {
				return releaseNotesComponent{}, microerror.Mask(err)
			}
			url = urlBuffer.String()
		}

		var lines []string
		{
			response, err := http.Get(url)
			if err != nil {
				return releaseNotesComponent{}, microerror.Mask(err)
			}

			body, err := ioutil.ReadAll(response.Body)
			if err != nil {
				return releaseNotesComponent{}, microerror.Mask(err)
			}

			lines = strings.Split(string(body), "\n")
		}

		var startPattern string
		{
			patternTemplate, err := template.New("link").Parse(params.start)
			if err != nil {
				return releaseNotesComponent{}, err
			}
			var buffer strings.Builder
			err = patternTemplate.Execute(&buffer, templateData)
			if err != nil {
				return releaseNotesComponent{}, err
			}
			startPattern = buffer.String()
		}

		var intermediatePattern string
		if params.intermediate != "" {
			patternTemplate, err := template.New("link").Parse(params.intermediate)
			if err != nil {
				return releaseNotesComponent{}, err
			}
			var buffer strings.Builder
			err = patternTemplate.Execute(&buffer, templateData)
			if err != nil {
				return releaseNotesComponent{}, err
			}
			intermediatePattern = buffer.String()
		}

		var endPattern string
		{
			patternTemplate, err := template.New("link").Parse(params.end)
			if err != nil {
				return releaseNotesComponent{}, err
			}
			var buffer strings.Builder
			err = patternTemplate.Execute(&buffer, templateData)
			if err != nil {
				return releaseNotesComponent{}, err
			}
			endPattern = buffer.String()
		}

		start := false
		intermediate := intermediatePattern == ""
		var changes []string
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}

			if !start {
				start, err = regexp.Match(startPattern, []byte(line))
				if err != nil {
					return releaseNotesComponent{}, microerror.Mask(err)
				}
				continue
			}

			if !intermediate {
				intermediate, err = regexp.Match(intermediatePattern, []byte(line))
				if err != nil {
					return releaseNotesComponent{}, microerror.Mask(err)
				}
				continue
			}

			end, err := regexp.Match(endPattern, []byte(line))
			if err != nil {
				return releaseNotesComponent{}, microerror.Mask(err)
			}
			if end {
				break
			}

			changes = append(changes, line)
		}

		result.Changelog = strings.Join(changes, "\n")
	}

	return result, nil
}

type containerLinuxSoftware struct {
	Docker   []string `json:"docker"`
	Ignition []string `json:"ignition"`
	Kernel   []string `json:"kernel"`
	Rkt      []string `json:"rkt"`
	Systemd  []string `json:"systemd"`
}

type containerLinuxRelease struct {
	Channel       string                 `json:"channel"`
	Architectures []string               `json:"architectures"`
	ReleaseDate   string                 `json:"release_date"`
	MajorSoftware containerLinuxSoftware `json:"major_software"`
	ReleaseNotes  string                 `json:"release_notes"`
}
