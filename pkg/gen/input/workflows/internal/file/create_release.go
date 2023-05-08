package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v2/pkg/gen/input"
	"github.com/giantswarm/devctl/v2/pkg/gen/input/workflows/internal/params"
)

//go:embed create_release.yaml.template
var createReleaseTemplate string

func NewCreateReleaseInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release.yaml"),
		TemplateBody: createReleaseTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":                         params.Header("#"),
			"EnableFloatingMajorVersionTags": params.EnableFloatingMajorVersionTags(p),
			"IsFlavourCLI":                   params.IsFlavourCLI(p),
			"StepSetUpGitIdentity":           params.StepSetUpGitIdentity(),
		},
	}

	return i
}
