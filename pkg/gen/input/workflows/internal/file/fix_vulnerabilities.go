package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed fix_vulnerabilities.yaml.template
var fixVulnerabilitiesTemplate string

//go:generate go run ../../../update-template-sha.go fix_vulnerabilities.yaml.template
//go:embed fix_vulnerabilities.yaml.template.sha
var fixVulnerabilitiesTemplateSha string

func NewFixVulnerabilitiesInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "fix_vulnerabilities.yaml"),
		TemplateBody: fixVulnerabilitiesTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":               params.Header("#", fixVulnerabilitiesTemplateSha),
			"StepSetUpGitIdentity": params.StepSetUpGitIdentity(),
		},
	}

	return i
}
