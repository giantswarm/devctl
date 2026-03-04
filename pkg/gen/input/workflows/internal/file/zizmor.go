package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed zizmor.yaml.template
var zizmorTemplate string

//go:generate go run ../../../update-template-sha.go zizmor.yaml.template
//go:embed zizmor.yaml.template.sha
var zizmorTemplateSha string

func NewZizmorInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "zizmor.yaml"),
		TemplateBody: zizmorTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", zizmorTemplateSha),
		},
	}

	return i
}
