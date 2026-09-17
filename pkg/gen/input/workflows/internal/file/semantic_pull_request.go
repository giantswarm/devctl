package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed semantic_pull_request.yaml.template
var semanticPullRequestTemplate string

//go:generate go run ../../../update-template-sha.go semantic_pull_request.yaml.template
//go:embed semantic_pull_request.yaml.template*
var semanticPullRequestTemplateFiles embed.FS

var semanticPullRequestTemplateSha = input.TemplateSHA(semanticPullRequestTemplateFiles, "semantic_pull_request.yaml.template")

func NewSemanticPullRequestInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "semantic_pull_request.yaml"),
		TemplateBody: semanticPullRequestTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", semanticPullRequestTemplateSha),
		},
	}

	return i
}
