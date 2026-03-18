package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed analyze_github_actions.yaml.template
var analyzeGithubActionsTemplate string

//go:generate go run ../../../update-template-sha.go analyze_github_actions.yaml.template
//go:embed analyze_github_actions.yaml.template.sha
var analyzeGithubActionsTemplateSha string

func NewAnalyzeGithubActionsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "analyze-github-actions.yaml"),
		TemplateBody: analyzeGithubActionsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", analyzeGithubActionsTemplateSha),
		},
	}

	return i
}
