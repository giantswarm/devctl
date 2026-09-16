package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed cluster_app_documentation_validation.yaml.template
var clusterAppDocumentationValidationTemplate string

//go:generate go run ../../../update-template-sha.go cluster_app_documentation_validation.yaml.template
//go:embed cluster_app_documentation_validation.yaml.template*
var clusterAppDocumentationValidationTemplateFiles embed.FS

var clusterAppDocumentationValidationTemplateSha = input.TemplateSHA(clusterAppDocumentationValidationTemplateFiles, "cluster_app_documentation_validation.yaml.template")

func NewClusterAppDocumentationValidation(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "documentation_validation.yaml"),
		TemplateBody: clusterAppDocumentationValidationTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", clusterAppDocumentationValidationTemplateSha),
		},
	}

	return i
}
