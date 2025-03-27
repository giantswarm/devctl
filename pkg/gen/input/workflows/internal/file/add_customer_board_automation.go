package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed add_customer_board_automation.yaml.template
var customerBoardAutomationTemplate string

//go:generate go run ../../../update-template-sha.go add_customer_board_automation.yaml.template
//go:embed add_customer_board_automation.yaml.template.sha
var customerBoardAutomationTemplateSha string

func NewCustomerBoardAutomationInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "add_customer_board_automation.yaml"),
		TemplateBody: customerBoardAutomationTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", customerBoardAutomationTemplateSha),
		},
	}

	return i
}
