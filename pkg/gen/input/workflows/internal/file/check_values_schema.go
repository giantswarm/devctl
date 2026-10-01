package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed check_values_schema.yaml.template
var checkValuesSchemaTemplate string

//go:generate go run ../../../update-template-sha.go check_values_schema.yaml.template
//go:embed check_values_schema.yaml.template*
var checkValuesSchemaTemplateFiles embed.FS

var checkValuesSchemaTemplateSha = input.TemplateSHA(checkValuesSchemaTemplateFiles, "check_values_schema.yaml.template")

func NewCheckValuesSchemaInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "check_values_schema.yaml"),
		TemplateBody: checkValuesSchemaTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", checkValuesSchemaTemplateSha),
			"SchemaDocsURL":   "https://intranet.giantswarm.io/docs/organizational-structure/teams/cabbage/app-updates/helm-values-schema/",
		},
	}

	return i
}
