package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed fix_vulnerabilities.yaml.template
var fixVulnerabilitiesTemplate string

//go:generate go run ../../../update-template-sha.go fix_vulnerabilities.yaml.template
//go:embed fix_vulnerabilities.yaml.template*
var fixVulnerabilitiesTemplateFiles embed.FS

var fixVulnerabilitiesTemplateSha = input.TemplateSHA(fixVulnerabilitiesTemplateFiles, "fix_vulnerabilities.yaml.template")

func NewFixVulnerabilitiesInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "fix_vulnerabilities.yaml"),
		TemplateBody: fixVulnerabilitiesTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader:               params.Header("#", fixVulnerabilitiesTemplateSha),
			templateKeyStepSetUpGitIdentity: params.StepSetUpGitIdentity(),
		},
	}

	return i
}
