package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed helm_render_diff.yaml.template
var helmRenderDiffTemplate string

//go:generate go run ../../../update-template-sha.go helm_render_diff.yaml.template
//go:embed helm_render_diff.yaml.template.sha
var helmRenderDiffTemplateSha string

func NewHelmRenderDiff(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "diff_helm_render_templates.yaml"),
		TemplateBody: helmRenderDiffTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", helmRenderDiffTemplateSha),
		},
	}

	return i
}
