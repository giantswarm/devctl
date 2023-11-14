package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed cluster_app_documentation_validation.yaml.template
var clusterAppDocumentationValidationTemplate string

func NewClusterAppDocumentationValidation(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "documentation_validation.yaml"),
		TemplateBody: clusterAppDocumentationValidationTemplate,
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
