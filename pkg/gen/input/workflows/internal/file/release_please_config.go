package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
)

//go:embed release_please_config.json.template
var releasePleaseConfigTemplate string

// NewReleasePleaseConfigInput returns a scaffolding Input (generate-once) for
// release-please-config.json in the repository root. Existing files are never
// overwritten so users can extend the config (e.g. add version-files).
func NewReleasePleaseConfigInput(changelogStyle string) input.Input {
	return input.Input{
		Path:         filepath.Join(".", "release-please-config.json"),
		TemplateBody: releasePleaseConfigTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"LegacyChangelog": changelogStyle != "release-please",
		},
	}
}
