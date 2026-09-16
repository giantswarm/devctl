package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed publish_techdocs.yaml.template
var publishTechdocsTemplate string

//go:generate go run ../../../update-template-sha.go publish_techdocs.yaml.template
//go:embed publish_techdocs.yaml.template*
var publishTechdocsTemplateFiles embed.FS

var publishTechdocsTemplateSha = input.TemplateSHA(publishTechdocsTemplateFiles, "publish_techdocs.yaml.template")

func NewPublishTechdocs(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "publish_techdocs.yaml"),
		TemplateBody: publishTechdocsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", publishTechdocsTemplateSha),
		},
	}

	return i
}
