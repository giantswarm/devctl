package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed auto_release.yaml.template
var autoReleaseTemplate string

//go:generate go run ../../../update-template-sha.go auto_release.yaml.template
//go:embed auto_release.yaml.template.sha
var autoReleaseTemplateSha string

func NewAutoReleaseInput(p params.Params) input.Input {
	return input.Input{
		Path:         params.RegenerableFileName(p, "auto-release.yaml"),
		TemplateBody: autoReleaseTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", autoReleaseTemplateSha),
		},
	}
}
