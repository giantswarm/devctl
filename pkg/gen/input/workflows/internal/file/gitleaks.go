package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed gitleaks.yaml.template
var gitleaksTemplate string

//go:generate go run ../../../update-template-sha.go gitleaks.yaml.template
//go:embed gitleaks.yaml.template.sha
var gitleaksTemplateSha string

func NewGitleaksInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "gitleaks.yaml"),
		TemplateBody: gitleaksTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", gitleaksTemplateSha),
		},
	}

	return i
}
