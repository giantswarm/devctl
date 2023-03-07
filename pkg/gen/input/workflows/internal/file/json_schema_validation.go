package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/workflows/internal/params"
)

//go:embed json_schema_validation.yaml.template
var jsonSchemaValidationTemplate string

func NewJSONSchemaValidation(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "json_schema_validation.yaml"),
		TemplateBody: jsonSchemaValidationTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
