package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

//go:embed update_chart.yaml.template
var updateChartTemplate string

func NewUpdateChartInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "update_chart.yaml"),
		TemplateBody: updateChartTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":               params.Header("#"),
			"StepSetUpGitIdentity": params.StepSetUpGitIdentity(),
		},
	}

	return i
}
