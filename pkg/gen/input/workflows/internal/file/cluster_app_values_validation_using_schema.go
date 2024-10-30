package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed cluster_app_values_validation_using_schema.yaml.template
var clusterAppValuesValidationUsingSchemaTemplate string

//go:generate go run ../../../update-template-sha.go cluster_app_values_validation_using_schema.yaml.template
//go:embed cluster_app_values_validation_using_schema.yaml.template.sha
var clusterAppValuesValidationUsingSchemaTemplateSha string

func NewClusterAppValuesValidationUsingSchemaTemplate(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "cluster_app_values_validation_schema.yaml"),
		TemplateBody: clusterAppValuesValidationUsingSchemaTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", clusterAppValuesValidationUsingSchemaTemplateSha),
		},
	}

	return i
}
