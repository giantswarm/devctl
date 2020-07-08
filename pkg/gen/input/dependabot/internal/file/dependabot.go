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
			"Interval":  p.Interval,
			"Reviewers": p.Reviewers,
		},
	}

	return i
}

var createDependabotTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen dependabot
#
version: 2
updates:
- package-ecosystem: gomod
  directory: "/"
  schedule:
    interval: {{ .Interval }}
    time: "04:00"
  open-pull-requests-limit: 10
{{- if .Reviewers }}
  reviewers:
  {{- range $reviewer:= .Reviewers }}
  - {{ $reviewer }}
  {{- end}}
{{- end }}
  ignore:
  - dependency-name: k8s.io/*
    versions:
    - ">=0.17.0"
- package-ecosystem: docker
  directory: "/"
  schedule:
    interval: {{ .Interval }}
    time: "04:00"
  target-branch: master
{{- if .Reviewers }}
  reviewers:
  {{- range $reviewer:= .Reviewers }}
  - {{ $reviewer }}
  {{- end}}
{{- end }}
`
