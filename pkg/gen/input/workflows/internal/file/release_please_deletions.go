package file

import (
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

// Cleanup inputs for the release-please flow, which devctl no longer
// generates. These remove the workflow + config + manifest files that prior
// release-please-enabled gen runs left behind in the 16 bumblebee repos that
// were on `releaseWorkflow: release-please` before that opt-in was reverted
// (see giantswarm/github). They get wired into the workflows runner so the
// next `align-files` cycle sweeps the stale files alongside generating the
// legacy workflows. Safe no-op for any repo that never had release-please.
// Remove this file and its wiring once every affected repo has been swept
// (track via `gh search code` for the file paths below).

func NewReleasePleaseDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "release-please.yaml"),
	}
}

func NewReleasePleaseConfigDeletionInput() input.Input {
	return input.Input{
		Delete: true,
		Path:   filepath.Join(".", "release-please-config.json"),
	}
}

func NewReleasePleaseManifestDeletionInput() input.Input {
	return input.Input{
		Delete: true,
		Path:   filepath.Join(".", ".release-please-manifest.json"),
	}
}
