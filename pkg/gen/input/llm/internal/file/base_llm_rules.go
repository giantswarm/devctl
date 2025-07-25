package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/llm/internal/params"
)

//go:embed base_llm_rules.mdc.template
var baseLLMRulesTemplate string

//go:generate go run ../../../update-template-sha.go base_llm_rules.mdc.template
//go:embed base_llm_rules.mdc.template.sha
var baseLLMRulesTemplateSha string

func NewBaseLLMRulesInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "base-llm-rules.mdc"),
		TemplateBody: baseLLMRulesTemplate,
		TemplateData: map[string]interface{}{
			"Header":       params.Header(baseLLMRulesTemplateSha),
			"IsLanguageGo": params.IsLanguageGo(p),
			"Language":     p.Language,
		},
	}

	return i
}
