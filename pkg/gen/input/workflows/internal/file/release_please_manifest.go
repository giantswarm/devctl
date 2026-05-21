package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
)

//go:embed release_please_manifest.json.template
var releasePleaseManifestTemplate string

// NewReleasePleaseManifestInput returns a scaffolding Input (generate-once) for
// .release-please-manifest.json in the repository root. Release Please updates
// this file on every run to track the current released version.
func NewReleasePleaseManifestInput() input.Input {
	return input.Input{
		Path:         filepath.Join(".", ".release-please-manifest.json"),
		TemplateBody: releasePleaseManifestTemplate,
	}
}
