package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed auto_release.yaml.template
var autoReleaseTemplate string

//go:generate go run ../../../update-template-sha.go auto_release.yaml.template
//go:embed auto_release.yaml.template.sha
var autoReleaseTemplateSha string

// NewAutoReleaseInput emits .github/workflows/zz_generated.auto_release.yaml --
// the push-based release tagger + GitHub-Release publisher used by the
// `releaseWorkflow: auto-release` flow. The `zz_generated.` prefix marks the
// file as regenerable so subsequent `devctl gen` runs overwrite it. Repos
// that adopted this flow before the rename still carry the un-prefixed
// `auto-release.yaml`; NewAutoReleaseLegacyDeletionInput removes it.
func NewAutoReleaseInput(p params.Params) input.Input {
	return input.Input{
		Path:         params.RegenerableFileName(p, "auto_release.yaml"),
		TemplateBody: autoReleaseTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#", autoReleaseTemplateSha),
		},
	}
}

// NewAutoReleaseDeletionInput returns an Input that deletes the file
// NewAutoReleaseInput would generate. Wired into the `legacy` branch in
// runner.go so a repo switched back from `auto-release` to `legacy` (or
// never on it in the first place) doesn't keep a stale workflow.
func NewAutoReleaseDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   params.RegenerableFileName(p, "auto_release.yaml"),
	}
}

// NewAutoReleaseLegacyDeletionInput deletes the un-prefixed
// `.github/workflows/auto-release.yaml` from repos that adopted the
// auto-release flow before the file was migrated to use the `zz_generated.`
// prefix. Wired into BOTH branches in runner.go so every regeneration
// cleans up the legacy path regardless of which flow the repo is on.
func NewAutoReleaseLegacyDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   filepath.Join(p.Dir, "auto-release.yaml"),
	}
}
