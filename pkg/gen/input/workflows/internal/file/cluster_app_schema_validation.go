package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed cluster_app_schema_validation.yaml.template
var clusterAppSchemaValidationTemplate string

//go:generate go run ../../../update-template-sha.go cluster_app_schema_validation.yaml.template
//go:embed cluster_app_schema_validation.yaml.template.sha
var clusterAppSchemaValidationTemplateSha string

func NewClusterAppSchemaValidation(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "json_schema_validation.yaml"),
		TemplateBody: clusterAppSchemaValidationTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", clusterAppSchemaValidationTemplateSha),
		},
	}

	return i
}
