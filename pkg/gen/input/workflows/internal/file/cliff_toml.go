package file

import (
	"context"
	"embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/internal/gitremote"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed cliff.toml.template
var cliffTomlTemplate string

//go:generate go run ../../../update-template-sha.go cliff.toml.template
//go:embed cliff.toml.template*
var cliffTomlTemplateFiles embed.FS

var cliffTomlTemplateSha = input.TemplateSHA(cliffTomlTemplateFiles, "cliff.toml.template")

// detectRepoName reads the repository name from the origin remote of the cwd
// (the consuming repo at gen time) for cliff.toml's `[remote.github].repo`
// field.
//
// Returns "" when it cannot (no .git directory, no origin remote, git not
// installed, a remote naming no <owner>/<name> repository). cliff.toml then
// renders with `repo = ""`, which makes git-cliff's GitHub API lookups fail
// loudly at workflow runtime -- a clearer signal than silently picking a
// wrong default.
func detectRepoName() string {
	remote, err := gitremote.OriginURL(context.Background(), ".")
	if err != nil {
		return ""
	}
	repo, err := gitremote.Parse(remote)
	if err != nil {
		return ""
	}
	return repo.Name
}

// NewCliffTomlInput emits cliff.toml at the repo root with
// [remote.github].repo auto-detected from the cwd's git remote.
//
// SkipRegenCheck forces regenerate-on-every-run despite the lack of a
// zz_generated. prefix (cliff.toml lives at repo root and shouldn't be
// renamed). Per-repo customizations to cliff.toml will be lost on the next
// gen run -- intentional, to keep all consumers on the canonical template.
func NewCliffTomlInput() input.Input {
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
			"RepoName":        detectRepoName(),
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
