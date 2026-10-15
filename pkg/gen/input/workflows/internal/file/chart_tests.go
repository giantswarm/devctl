package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed chart_tests.yaml.template
var chartTestsTemplate string

//go:generate go run ../../../update-template-sha.go chart_tests.yaml.template
//go:embed chart_tests.yaml.template*
var chartTestsTemplateFiles embed.FS

var chartTestsTemplateSha = input.TemplateSHA(chartTestsTemplateFiles, "chart_tests.yaml.template")

func NewChartTestsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "chart_tests.yaml"),
		TemplateBody: chartTestsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", chartTestsTemplateSha),
		},
	}

	return i
}
