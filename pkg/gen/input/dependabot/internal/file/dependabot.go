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
			"EcosystemGomod": params.EcosystemGomod(p),
			"Ecosystems":     params.Ecosystems(p),
			"Header":         params.Header("#"),
			"Interval":       params.Interval(p),
			"Reviewers":      params.Reviewers(p),
		},
	}

	return i
}

var createDependabotTemplate = `{{ .Header }}
{{- $interval := .Interval }}
{{- $ecosystemGomod := .EcosystemGomod }}
{{- $reviewers := .Reviewers }}
version: 2
updates:
{{- range $ecosystem := .Ecosystems }}
  - package-ecosystem: {{ $ecosystem }}
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
  {{- if eq $ecosystem $ecosystemGomod }}
    ignore:
      - dependency-name: k8s.io/*
        versions:
          - ">=0.19.0"
{{- end }}
{{- end }}
`
