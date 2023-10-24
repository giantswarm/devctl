package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed create_release_pr.yaml.template
var createReleasePRTemplate string

func NewCreateReleasePRInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release_pr.yaml"),
		TemplateBody: createReleasePRTemplate,
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
