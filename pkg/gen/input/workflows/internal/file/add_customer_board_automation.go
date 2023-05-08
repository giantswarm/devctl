package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v2/pkg/gen/input"
	"github.com/giantswarm/devctl/v2/pkg/gen/input/workflows/internal/params"
)

//go:embed add_customer_board_automation.yaml.template
var customerBoardAutomationTemplate string

func NewCustomerBoardAutomationInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "add_customer_board_automation.yaml"),
		TemplateBody: customerBoardAutomationTemplate,
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
