package file

import (
	"embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed cliff.toml.template
var cliffTomlTemplate string

//go:generate go run ../../../update-template-sha.go cliff.toml.template
//go:embed cliff.toml.template*
var cliffTomlTemplateFiles embed.FS

var cliffTomlTemplateSha = input.TemplateSHA(cliffTomlTemplateFiles, "cliff.toml.template")

// NewCliffTomlInput emits cliff.toml at the repo root with
// [remote.github].repo set to p.RepoName: the caller's own name, read from
// the origin remote of a real checkout or an explicit --repo-name override
// where none exists (a scaffold rendered into a bare directory). devctl gen
// workflows refuses to run with RepoName empty, so this never silently
// writes `repo = ""`.
//
// SkipRegenCheck forces regenerate-on-every-run despite the lack of a
// zz_generated. prefix (cliff.toml lives at repo root and shouldn't be
// renamed). Per-repo customizations to cliff.toml will be lost on the next
// gen run -- intentional, to keep all consumers on the canonical template.
func NewCliffTomlInput(p params.Params) input.Input {
	return input.Input{
		Path:           filepath.Join(".", "cliff.toml"),
		SkipRegenCheck: true,
		TemplateBody:   cliffTomlTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  templateDelimLeft,
			Right: templateDelimRight,
		},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", cliffTomlTemplateSha),
			"RepoName":        p.RepoName,
		},
	}
}

// NewCliffTomlDeletionInput returns an Input that deletes cliff.toml. Wired
// into the `legacy` branch so a repo switched from `auto-release` back to
// `legacy` doesn't keep a stale cliff.toml.
func NewCliffTomlDeletionInput() input.Input {
	return input.Input{
		Delete: true,
		Path:   filepath.Join(".", "cliff.toml"),
	}
}
