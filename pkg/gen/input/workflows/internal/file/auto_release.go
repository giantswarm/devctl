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

// NewAutoReleaseInput emits .github/workflows/auto-release.yaml -- the
// push-based release tagger + GitHub-Release publisher used by the
// `releaseWorkflow: auto-release` flow.
//
// The file is NOT prefixed with `zz_generated.` because the same path is
// already in use across the 13+ repos manually migrated to this flow. Using
// the prefix would cause both files to coexist and race. `SkipRegenCheck`
// forces overwrite-on-every-run, restoring the regenerable-file behavior the
// prefix would otherwise provide.
func NewAutoReleaseInput(p params.Params) input.Input {
	return input.Input{
		Path:           filepath.Join(p.Dir, "auto-release.yaml"),
		SkipRegenCheck: true,
		TemplateBody:   autoReleaseTemplate,
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
// never on it in the first place) doesn't keep a stale auto-release.yaml.
func NewAutoReleaseDeletionInput(p params.Params) input.Input {
	return input.Input{
		Delete: true,
		Path:   filepath.Join(p.Dir, "auto-release.yaml"),
	}
}
