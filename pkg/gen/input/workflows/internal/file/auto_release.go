package file

import (
	"embed"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed auto_release.yaml.template
var autoReleaseTemplate string

//go:generate go run ../../../update-template-sha.go auto_release.yaml.template
//go:embed auto_release.yaml.template*
var autoReleaseTemplateFiles embed.FS

var autoReleaseTemplateSha = input.TemplateSHA(autoReleaseTemplateFiles, "auto_release.yaml.template")

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
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader:              params.Header("#", autoReleaseTemplateSha),
			templateKeyFork:                params.IsFlavourFork(p),
			templateKeyMaintenanceBranches: params.IsFlavourFork(p) && p.MaintenanceBranches,
			templateKeyPatchCandidates:     p.PatchCandidates,
			templateKeyReleaseBranch:       p.ReleaseBranch,
			templateKeyAdvisoryJobs:        strings.Join(circleciclient.AdvisoryJobs, " "),
		},
	}
}

//go:embed auto_release_multi_image.yaml.template
var autoReleaseMultiImageTemplate string

//go:generate go run ../../../update-template-sha.go auto_release_multi_image.yaml.template
//go:embed auto_release_multi_image.yaml.template*
var autoReleaseMultiImageTemplateFiles embed.FS

var autoReleaseMultiImageTemplateSha = input.TemplateSHA(autoReleaseMultiImageTemplateFiles, "auto_release_multi_image.yaml.template")

// NewAutoReleaseMultiImageInput emits the zz_generated.auto_release.yaml of a
// multi-image repository (flavour multi-image) in place of the single-line
// one: on every push it tags each image whose directory changed since its
// latest <name>/vX.Y.Z tag with its next version, from the pushed commit's
// subject. Same path as NewAutoReleaseInput, so the deletion inputs of both
// release flows cover it.
func NewAutoReleaseMultiImageInput(p params.Params) input.Input {
	return input.Input{
		Path:         params.RegenerableFileName(p, "auto_release.yaml"),
		TemplateBody: autoReleaseMultiImageTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader:        params.Header("#", autoReleaseMultiImageTemplateSha),
			templateKeyReleaseBranch: p.ReleaseBranch,
			"ImagesDir":              gen.MultiImageDir,
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
