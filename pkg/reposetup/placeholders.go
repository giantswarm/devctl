package reposetup

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
)

// scaffoldFiles are the files every Giant Swarm repository carries, as
// giantswarm/github's repositories/default has them: the align-files run
// overwrites them with the same content.
//
//go:embed scaffold/DCO scaffold/LICENSE scaffold/SECURITY.md
var scaffoldFiles embed.FS

// substitutions are the values the placeholders take.
type substitutions struct {
	// Name of the repository.
	Name string
	// Team slug (team-bumblebee) for CODEOWNERS.
	Team string
	// Description of the repository for the README.
	Description string
	// UpstreamRepo credited in the chart README; the repository's own URL
	// when the chart follows no upstream.
	UpstreamRepo string
}

// teamShortName is the team's name without the team- prefix, the form the
// chart's team annotation carries (shield for team-shield).
func (s substitutions) teamShortName() string {
	return strings.TrimPrefix(s.Team, "team-")
}

// replacement is one placeholder: what `devctl replace` would be run with.
type replacement struct {
	pattern *regexp.Regexp
	value   string
}

// appNamePlaceholder is the braced name token of template-app and
// template-plans, also in path names (helm/{APP-NAME}).
const appNamePlaceholder = "{APP-NAME}"

// replacements returns the template's placeholders: one replacement pass
// serves every template, whichever token form it carries.
//
// giantswarm/template, the Go service template, carries the brace-less
// REPOSITORY_NAME: a Go module path may not contain braces, and the
// template's own build runs on its go.mod (module
// github.com/giantswarm/REPOSITORY_NAME). template-app carries the braced
// {APP-NAME}, {TEAM-NAME} and {APP HELM REPOSITORY}, template-plans the
// first two. Each token is matched literally, so both forms are replaced
// the same way and a repository is created from either template the same
// way; only when copying by hand does the pattern differ, one
// `devctl replace` per token (REPOSITORY_NAME for the Go template,
// {APP-NAME} for template-app). The Go table takes {APP-NAME} as well, and
// sets the binary of the go-build job in the template's own
// .circleci/config.yml (binary: template), which is no token.
//
// {TEAMS} and {BOARD} in the issue-automation workflows are not
// placeholders: those files are align-files' verbatim copies and the
// workflows read them at run time.
func replacements(t Template, s substitutions) []replacement {
	appName := replacement{regexp.MustCompile(regexp.QuoteMeta(appNamePlaceholder)), s.Name}
	switch t {
	case TemplateGo:
		return []replacement{
			{regexp.MustCompile(`REPOSITORY_NAME`), s.Name},
			appName,
			// The binary the template's own CircleCI config builds; the
			// generated config replaces the file when ci.generate is on.
			{regexp.MustCompile(`(?m)^(\s*binary:\s*)template\s*$`), "${1}" + s.Name},
		}
	case TemplateChart:
		return []replacement{
			appName,
			{regexp.MustCompile(regexp.QuoteMeta("{TEAM-NAME}")), s.teamShortName()},
			{regexp.MustCompile(regexp.QuoteMeta("{APP HELM REPOSITORY}")), s.UpstreamRepo},
		}
	case TemplatePlans:
		return []replacement{
			appName,
			{regexp.MustCompile(regexp.QuoteMeta("{TEAM-NAME}")), s.teamShortName()},
		}
	default:
		return nil
	}
}

// replacePlaceholders renames the paths and rewrites the files carrying the
// template's placeholders. A symlink is never read for content replacement
// (d.Type().IsRegular() is false for it) and is left exactly as the template
// extracted it, target included — a link whose target string itself carries
// a placeholder (none of the current templates' do) would stay unreplaced.
func replacePlaceholders(dir string, t Template, s substitutions) error {
	if err := renamePlaceholderPaths(dir, s.Name); err != nil {
		return microerror.Mask(err)
	}

	repl := replacements(t, s)
	if len(repl) == 0 {
		return nil
	}

	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		if d.IsDir() {
			if d.Name() == gitDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// A symlink (or anything else that isn't a plain file): its
			// content is the target it was extracted with, not this
			// template's text, and os.ReadFile below would follow it and
			// rewrite whatever it points at instead.
			return nil
		}
		content, err := os.ReadFile(p) // #nosec G304 G122 -- walking the scaffold directory this package just created from the template; nothing else writes there
		if err != nil {
			return microerror.Mask(err)
		}
		replaced := content
		for _, r := range repl {
			replaced = r.pattern.ReplaceAll(replaced, []byte(r.value))
		}
		if bytes.Equal(content, replaced) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return microerror.Mask(err)
		}
		return writeFile(p, replaced, info.Mode().Perm())
	})
}

// renamePlaceholderPaths renames every path segment equal to {APP-NAME},
// deepest first.
func renamePlaceholderPaths(dir, name string) error {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		if d.Name() == appNamePlaceholder {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return microerror.Mask(err)
	}

	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, p := range paths {
		if err := os.Rename(p, filepath.Join(filepath.Dir(p), name)); err != nil {
			return microerror.Mask(err)
		}
	}
	return nil
}

// Codeowners is the CODEOWNERS file of a repository owned by team (a team
// slug such as team-bumblebee), byte-identical to what align-files writes.
func Codeowners(team string) string {
	return fmt.Sprintf("# generated by giantswarm/github actions - changes will be overwritten\n* @giantswarm/%s\n", team)
}

// writeCommonFiles writes what every scaffold carries whatever its
// template: CODEOWNERS as align-files writes it, and for a template without
// a repository-specific README ([Template.shipsReadme] false — the Go
// template's README describes the template itself) the generic README. The
// minimal scaffold gets LICENSE, DCO, SECURITY.md and .gitignore as well.
func writeCommonFiles(dir string, t Template, s substitutions) error {
	if err := writeFile(filepath.Join(dir, "CODEOWNERS"), []byte(Codeowners(s.Team)), fileMode); err != nil {
		return microerror.Mask(err)
	}

	if !t.shipsReadme() {
		if err := writeFile(filepath.Join(dir, "README.md"), []byte(readme(s)), fileMode); err != nil {
			return microerror.Mask(err)
		}
	}

	if t != TemplateMinimal {
		return nil
	}

	for _, name := range []string{"DCO", "LICENSE", "SECURITY.md"} {
		data, err := scaffoldFiles.ReadFile("scaffold/" + name)
		if err != nil {
			return microerror.Mask(err)
		}
		if err := writeFile(filepath.Join(dir, name), data, fileMode); err != nil {
			return microerror.Mask(err)
		}
	}
	if err := writeFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), fileMode); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// readme is the README of a repository whose template has none of its own.
func readme(s substitutions) string {
	description := s.Description
	if description == "" {
		description = fmt.Sprintf("TODO: describe what %s is for.", s.Name)
	}
	return fmt.Sprintf("# %s\n\n%s\n", s.Name, description)
}

// gitignore is the minimal scaffold's .gitignore: editor and OS files.
const gitignore = `# Editor and OS files
.idea/
.vscode/
*.swp
.DS_Store
`
