package file

import (
	"path/filepath"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/dependabot/internal/params"
)

func NewCreateDependabotInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "dependabot.yml"),
		TemplateBody: createDependabotTemplate,
		TemplateData: map[string]interface{}{
			"Ecosystems": p.Ecosystems,
			"Interval":   p.Interval,
			"Reviewers":  p.Reviewers,
		},
	}

	return i
}

var createDependabotTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen dependabot
#
{{- $interval := .Interval }}
{{- $reviewers := .Reviewers }}
version: 2
updates:
{{- range $ecosystem := .Ecosystems }}
{{- if eq $ecosystem "go" }}
- package-ecosystem: gomod
  directory: "/"
  schedule:
    interval: {{ $interval }}
    time: "04:00"
  open-pull-requests-limit: 10
{{- if $reviewers }}
  reviewers:
  {{- range $reviewer := $reviewers }}
  - {{ $reviewer }}
  {{- end}}
{{- end }}
  ignore:
  - dependency-name: k8s.io/*
    versions:
    - ">=0.17.0"
{{- end }}
{{- if eq $ecosystem "docker" }}
- package-ecosystem: docker
  directory: "/"
  schedule:
    interval: {{ $interval }}
    time: "04:00"
  target-branch: master
{{- if $reviewers }}
  reviewers:
  {{- range $reviewer := $reviewers }}
  - {{ $reviewer }}
  {{- end}}
{{- end }}
{{- end }}
{{- end }}
`
