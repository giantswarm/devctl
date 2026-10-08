package file

import (
	_ "embed"
	"path/filepath"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/renovate/internal/params"
)

// presetsPrefix scopes every extra extends entry to giantswarm/renovate-presets.
const presetsPrefix = "github>giantswarm/renovate-presets:"

//go:embed renovate.json5.template
var createRenovateTemplate string

// squoteEscaper escapes the characters that would otherwise break a
// single-quoted JSON5 string literal. GitHub reviewer/team slugs can't contain
// these, but --interval is free-text, so values are escaped defensively to keep
// the generated file valid no matter what is passed in.
var squoteEscaper = strings.NewReplacer(
	`\`, `\\`,
	`'`, `\'`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

// squote renders s as a single-quoted JSON5 string literal. The template emits
// the result verbatim, so all quoting/escaping lives here rather than in the
// template (where text/template's printf has no single-quote-escaping verb).
func squote(s string) string {
	return "'" + squoteEscaper.Replace(s) + "'"
}

func NewCreateRenovateInput(p params.Params) input.Input {
	// Pre-quote the free-text values into JSON5 string literals here so the
	// template can emit them verbatim. Interval stays empty (not quoted) when
	// unset so the template's `ne .Interval ""` guard still omits the key.
	interval := params.Interval(p)
	if interval != "" {
		interval = squote(interval)
	}

	reviewers := params.Reviewers(p)
	quotedReviewers := make([]string, len(reviewers))
	for i, r := range reviewers {
		quotedReviewers[i] = squote(r)
	}

	extends := params.Extends(p)
	quotedExtends := make([]string, len(extends))
	for i, e := range extends {
		quotedExtends[i] = squote(presetsPrefix + e)
	}

	i := input.Input{
		Path:         filepath.Join(p.Dir, "renovate.json5"),
		TemplateBody: createRenovateTemplate,
		TemplateData: map[string]interface{}{
			"Interval":          interval,
			"Language":          params.Language(p),
			"Reviewers":         quotedReviewers,
			"CircleCIGenerated": params.CircleCIGenerated(p),
			"RepoName":          params.RepoName(p),
			"HasCustomConfig":   params.HasCustomConfig(p),
			"Deprecated":        params.Deprecated(p),
			"Extends":           quotedExtends,
		},
	}

	return i
}
