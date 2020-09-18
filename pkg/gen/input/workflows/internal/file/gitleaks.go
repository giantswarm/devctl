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
	}

	return i
}

var gitleaksTemplate = `# DO NOT EDIT. Generated with:
#
#    devctl gen workflows
#
name: gitleaks

on: [push,pull_request]

jobs:
  gitleaks:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v2
      with:
        fetch-depth: '0'
    - name: gitleaks-action
      uses: zricethezav/gitleaks-action@v1.1.4
`
