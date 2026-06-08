package file

import (
	_ "embed"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

//go:embed cliff.toml.template
var cliffTomlTemplate string

//go:generate go run ../../../update-template-sha.go cliff.toml.template
//go:embed cliff.toml.template.sha
var cliffTomlTemplateSha string

// detectRepoName runs `git config --get remote.origin.url` in the cwd
// (which is the consuming repo at gen time) and extracts the bare repo name
// from the URL. Used to populate cliff.toml's `[remote.github].repo` field.
//
// Supports the three URL forms git remote produces:
//
//	git@github.com:giantswarm/foo.git
//	https://github.com/giantswarm/foo.git
//	https://github.com/giantswarm/foo
//
// Returns "" on any error (no .git directory, no origin remote, git not
// installed). cliff.toml then renders with `repo = ""`, which makes
// git-cliff's GitHub API lookups fail loudly at workflow runtime -- a
// clearer signal than silently picking a wrong default.
func detectRepoName() string {
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	if i := strings.LastIndex(url, "/"); i >= 0 {
		url = url[i+1:]
	}
	return strings.TrimSuffix(url, ".git")
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
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header":   params.Header("#", cliffTomlTemplateSha),
			"RepoName": detectRepoName(),
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
