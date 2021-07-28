package file

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

func NewGitleaksInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "gitleaks.yaml"),
		TemplateBody: gitleaksTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}

var gitleaksTemplate = `{{{{ .Header }}}}
name: gitleaks

on: [pull_request]

jobs:
  gitleaks:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v2
      with:
        fetch-depth: '0'
    - name: gitleaks-action
      uses: zricethezav/gitleaks-action@v1.6.0
`
