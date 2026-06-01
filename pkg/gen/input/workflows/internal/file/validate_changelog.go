package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed validate_changelog.yaml.template
var validateChangelogTemplate string

//go:generate go run ../../../update-template-sha.go validate_changelog.yaml.template
//go:embed validate_changelog.yaml.template.sha
var validateChangelogTemplateSha string

func NewValidateChangelogInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "validate_changelog.yaml"),
		TemplateBody: validateChangelogTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", validateChangelogTemplateSha),
		},
	}

	return i
}

// NewValidateChangelogDeletionInput returns an Input that deletes the file
// NewValidateChangelogInput would generate. Used when switching a repo to the
// release-please flow so the legacy workflow is removed in the same gen run.
func NewValidateChangelogDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "validate_changelog.yaml"),
	}
}
