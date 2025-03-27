package file

import (
	_ "embed"
	"strings"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed create_release.yaml.template
var createReleaseTemplate string

//go:generate go run ../../../update-template-sha.go create_release.yaml.template
//go:embed create_release.yaml.template.sha
var createReleaseTemplateSha string

func NewCreateReleaseInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release.yaml"),
		TemplateBody: createReleaseTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":                         params.Header("#", createReleaseTemplateSha),
			"EnableFloatingMajorVersionTags": params.EnableFloatingMajorVersionTags(p),
			"IsFlavourCLI":                   params.IsFlavourCLI(p),
			"StepSetUpGitIdentity":           params.StepSetUpGitIdentity(),
			"IsDevctl":                       strings.HasPrefix(createReleaseTemplateSha, "https://github.com/giantswarm/devctl"),
		},
	}

	return i
}
