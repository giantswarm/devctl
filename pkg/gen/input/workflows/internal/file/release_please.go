package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed release_please.yaml.template
var releasePleaseTemplate string

//go:generate go run ../../../update-template-sha.go release_please.yaml.template
//go:embed release_please.yaml.template.sha
var releasePleaseTemplateSha string

func NewReleasePleaseInput(p params.Params) input.Input {
	return input.Input{
		Path:         params.RegenerableFileName(p, "release-please.yaml"),
		TemplateBody: releasePleaseTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", releasePleaseTemplateSha),
		},
	}
}
