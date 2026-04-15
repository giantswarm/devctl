package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed dispatch_update_chart_events.yaml.template
var dispatchUpdateChartEventsTemplate string

//go:generate go run ../../../update-template-sha.go dispatch_update_chart_events.yaml.template
//go:embed dispatch_update_chart_events.yaml.template.sha
var dispatchUpdateChartEventsTemplateSha string

func NewDispatchUpdateChartEventsInput(p params.Params, targetRepo string) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "dispatch_update_chart_events.yaml"),
		TemplateBody: dispatchUpdateChartEventsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":           params.Header("#", dispatchUpdateChartEventsTemplateSha),
			"TargetRepository": targetRepo,
		},
	}

	return i
}
