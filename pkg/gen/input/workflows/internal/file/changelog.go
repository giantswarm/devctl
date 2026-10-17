package file

import (
	"embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed changelog.yaml.template
var changelogTemplate string

//go:generate go run ../../../update-template-sha.go changelog.yaml.template
//go:embed changelog.yaml.template*
var changelogTemplateFiles embed.FS

var changelogTemplateSha = input.TemplateSHA(changelogTemplateFiles, "changelog.yaml.template")

// NewChangelogInput emits .github/workflows/zz_generated.changelog.yaml, the
// pull request check that fails while released entries remain under
// Unreleased, for a repository on the auto-release workflow.
func NewChangelogInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "changelog.yaml"),
		TemplateBody: changelogTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", changelogTemplateSha),
		},
	}

	return i
}

// NewChangelogDeletionInput deletes the file NewChangelogInput generates from
// a repository on the legacy release workflow, whose release pull request
// writes the CHANGELOG.md sections itself.
func NewChangelogDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "changelog.yaml"),
	}
}
