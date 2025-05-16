package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed publish_techdocs.yaml.template
var publishTechdocsTemplate string

//go:generate go run ../../../update-template-sha.go publish_techdocs.yaml.template
//go:embed publish_techdocs.yaml.template.sha
var publishTechdocsTemplateSha string

func NewPublishTechdocs(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "publish_techdocs.yaml"),
		TemplateBody: publishTechdocsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", publishTechdocsTemplateSha),
		},
	}

	return i
}
