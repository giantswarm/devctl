package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed trigger_circleci_pipeline.yaml.template
var triggerCircleCIPipelineTemplate string

//go:generate go run ../../../update-template-sha.go trigger_circleci_pipeline.yaml.template
//go:embed trigger_circleci_pipeline.yaml.template.sha
var triggerCircleCIPipelineTemplateSha string

// NewTriggerCircleCIPipelineInput generates the caller of
// giantswarm/github-workflows' reusable trigger-circleci-pipeline.yaml (slice
// 03b): CircleCI builds on push, not on pull request open, so a branch pushed
// before its pull request existed is never built. This caller fires on
// pull_request opened/reopened and asks CircleCI's API to start that build.
func NewTriggerCircleCIPipelineInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "trigger-circleci-pipeline.yaml"),
		TemplateBody: triggerCircleCIPipelineTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", triggerCircleCIPipelineTemplateSha),
		},
	}

	return i
}
