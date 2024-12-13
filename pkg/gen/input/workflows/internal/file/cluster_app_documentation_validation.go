package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed cluster_app_documentation_validation.yaml.template
var clusterAppDocumentationValidationTemplate string

//go:generate go run ../../../update-template-sha.go cluster_app_documentation_validation.yaml.template
//go:embed cluster_app_documentation_validation.yaml.template.sha
var clusterAppDocumentationValidationTemplateSha string

func NewClusterAppDocumentationValidation(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "documentation_validation.yaml"),
		TemplateBody: clusterAppDocumentationValidationTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", clusterAppDocumentationValidationTemplateSha),
		},
	}

	return i
}
