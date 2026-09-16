package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed cluster_app_schema_validation.yaml.template
var clusterAppSchemaValidationTemplate string

//go:generate go run ../../../update-template-sha.go cluster_app_schema_validation.yaml.template
//go:embed cluster_app_schema_validation.yaml.template*
var clusterAppSchemaValidationTemplateFiles embed.FS

var clusterAppSchemaValidationTemplateSha = input.TemplateSHA(clusterAppSchemaValidationTemplateFiles, "cluster_app_schema_validation.yaml.template")

func NewClusterAppSchemaValidation(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "json_schema_validation.yaml"),
		TemplateBody: clusterAppSchemaValidationTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", clusterAppSchemaValidationTemplateSha),
		},
	}

	return i
}
