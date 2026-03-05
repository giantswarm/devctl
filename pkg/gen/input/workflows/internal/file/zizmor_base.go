package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed zizmor_base.yml.template
var zizmorBaseTemplate string

//go:generate go run ../../../update-template-sha.go zizmor_base.yml.template
//go:embed zizmor_base.yml.template.sha
var zizmorBaseTemplateSha string

func NewZizmorBaseInput(p params.Params) input.Input {
	i := input.Input{
		// Hardcoding .github/zizmor.base.yml as it belongs in .github, not .github/workflows
		Path:           ".github/zizmor.base.yml",
		TemplateBody:   zizmorBaseTemplate,
		SkipRegenCheck: true, // we omit zz_generated. prefix but still want to regenerate
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", zizmorBaseTemplateSha),
		},
	}

	return i
}
