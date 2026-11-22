package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed nested_go_modules.yaml.template
var nestedGoModulesTemplate string

//go:generate go run ../../../update-template-sha.go nested_go_modules.yaml.template
//go:embed nested_go_modules.yaml.template*
var nestedGoModulesTemplateFiles embed.FS

var nestedGoModulesTemplateSha = input.TemplateSHA(nestedGoModulesTemplateFiles, "nested_go_modules.yaml.template")

func NewNestedGoModulesInput(p params.Params) input.Input {
	return input.Input{
		Path:           params.RegenerableFileName(p, "nested_go_modules.yaml"),
		TemplateBody:   nestedGoModulesTemplate,
		TemplateDelims: input.InputTemplateDelims{Left: templateDelimLeft, Right: templateDelimRight},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", nestedGoModulesTemplateSha),
			"ReleaseBranch":   p.ReleaseBranch,
		},
	}
}
