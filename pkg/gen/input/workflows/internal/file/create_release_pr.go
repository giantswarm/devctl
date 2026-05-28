package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows/internal/params"
)

//go:embed create_release_pr.yaml.template
var createReleasePRTemplate string

//go:generate go run ../../../update-template-sha.go create_release_pr.yaml.template
//go:embed create_release_pr.yaml.template.sha
var createReleasePRTemplateSha string

func NewCreateReleasePRInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "create_release_pr.yaml"),
		TemplateBody: createReleasePRTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":               params.Header("#", createReleasePRTemplateSha),
			"StepSetUpGitIdentity": params.StepSetUpGitIdentity(),
		},
	}

	return i
}

// NewCreateReleasePRDeletionInput returns an Input that deletes the file
// NewCreateReleasePRInput would generate. Used when switching a repo to the
// release-please flow so the legacy workflow is removed in the same gen run.
func NewCreateReleasePRDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "create_release_pr.yaml"),
	}
}
