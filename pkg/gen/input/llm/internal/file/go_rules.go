package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/llm/internal/params"
)

//go:embed go_rules.mdc.template
var goRulesTemplate string

//go:generate go run ../../../update-template-sha.go go_rules.mdc.template
//go:embed go_rules.mdc.template*
var goRulesTemplateFiles embed.FS

var goRulesTemplateSha = input.TemplateSHA(goRulesTemplateFiles, "go_rules.mdc.template")

func NewGoLLMRulesInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "go-llm-rules.mdc"),
		TemplateBody: goRulesTemplate,
		TemplateData: map[string]interface{}{
			"Header": params.Header(goRulesTemplateSha),
		},
	}

	return i
}
