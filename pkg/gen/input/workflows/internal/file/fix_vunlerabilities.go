package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed fix_vulnerabilities.yaml.template
var fixVulnerabilitiesTemplate string

func NewFixVulnerabilitiesInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "fix_vulnerabilities.yaml"),
		TemplateBody: fixVulnerabilitiesTemplate,
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
