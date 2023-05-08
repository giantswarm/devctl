package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v2/pkg/gen/input"
	"github.com/giantswarm/devctl/v2/pkg/gen/input/workflows/internal/params"
)

//go:embed check_values_schema.yaml.template
var checkValuesSchemaTemplate string

func NewCheckValuesSchemaInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "check_values_schema.yaml"),
		TemplateBody: checkValuesSchemaTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":        params.Header("#"),
			"SchemaDocsURL": "https://intranet.giantswarm.io/docs/organizational-structure/teams/cabbage/app-updates/helm-values-schema/",
		},
	}

	return i
}
