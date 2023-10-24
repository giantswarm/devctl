package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
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
			"VendirVersion":        "v0.32.2",
			"VendirSha":            "f5d3cbbd8135d2d48f4f007b8a933bd60b2a827d68f4001c5d1774392fa7b3f2",
		},
	}

	return i
}
