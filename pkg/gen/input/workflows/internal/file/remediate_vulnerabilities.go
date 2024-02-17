package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed remediate_vulnerabilities.yaml.template
var remediateVulnerabilitiesTemplate string

func NewRemediateVulnerabilitiesInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "remediate_vulnerabilities.yaml"),
		TemplateBody: remediateVulnerabilitiesTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":               params.Header("#"),
			"StepSetUpGitIdentity": params.StepSetUpGitIdentity(),
		},
	}

	return i
}
