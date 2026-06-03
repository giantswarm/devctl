package file

import (
	_ "embed"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
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
			"Header":       params.Header("#", createReleaseTemplateSha),
			"IsFlavourCLI": params.IsFlavourCLI(p),
			"IsDevctl":     strings.HasPrefix(createReleaseTemplateSha, "https://github.com/giantswarm/devctl"),
		},
	}

	return i
}

// NewCreateReleaseDeletionInput returns an Input that deletes the file
// NewCreateReleaseInput would generate. Reusable when a repo opts into an
// alternative release flow that supersedes create-release so the legacy
// workflow is removed in the same gen run. Currently unused (the only
// alternative — release-please — was reverted) but kept as infrastructure
// for the planned push-based git-cliff flow.
func NewCreateReleaseDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "create_release.yaml"),
	}
}
