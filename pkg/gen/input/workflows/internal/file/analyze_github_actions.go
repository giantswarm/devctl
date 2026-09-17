package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed analyze_github_actions.yaml.template
var analyzeGithubActionsTemplate string

//go:generate go run ../../../update-template-sha.go analyze_github_actions.yaml.template
//go:embed analyze_github_actions.yaml.template*
var analyzeGithubActionsTemplateFiles embed.FS

var analyzeGithubActionsTemplateSha = input.TemplateSHA(analyzeGithubActionsTemplateFiles, "analyze_github_actions.yaml.template")

func NewAnalyzeGithubActionsInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "analyze-github-actions.yaml"),
		TemplateBody: analyzeGithubActionsTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", analyzeGithubActionsTemplateSha),
		},
	}

	return i
}
