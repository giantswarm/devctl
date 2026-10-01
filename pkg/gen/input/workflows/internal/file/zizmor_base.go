package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed zizmor_base.yml.template
var zizmorBaseTemplate string

//go:generate go run ../../../update-template-sha.go zizmor_base.yml.template
//go:embed zizmor_base.yml.template*
var zizmorBaseTemplateFiles embed.FS

var zizmorBaseTemplateSha = input.TemplateSHA(zizmorBaseTemplateFiles, "zizmor_base.yml.template")

func NewZizmorBaseInput(p params.Params) input.Input {
	i := input.Input{
		Path:           ".github/zizmor.base.yml",
		TemplateBody:   zizmorBaseTemplate,
		SkipRegenCheck: true,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", zizmorBaseTemplateSha),
		},
	}

	return i
}
