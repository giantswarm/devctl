package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed update_chart.yaml.template
var updateChartTemplate string

//go:generate go run ../../../update-template-sha.go update_chart.yaml.template
//go:embed update_chart.yaml.template*
var updateChartTemplateFiles embed.FS

var updateChartTemplateSha = input.TemplateSHA(updateChartTemplateFiles, "update_chart.yaml.template")

func NewUpdateChartInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "update_chart.yaml"),
		TemplateBody: updateChartTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", updateChartTemplateSha),
		},
	}

	return i
}
