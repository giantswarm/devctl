package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed semantic_pull_request.yaml.template
var semanticPullRequestTemplate string

//go:generate go run ../../../update-template-sha.go semantic_pull_request.yaml.template
//go:embed semantic_pull_request.yaml.template.sha
var semanticPullRequestTemplateSha string

func NewSemanticPullRequestInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "semantic_pull_request.yaml"),
		TemplateBody: semanticPullRequestTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", semanticPullRequestTemplateSha),
		},
	}

	return i
}
