package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/workflows/internal/params"
)

//go:embed lint_changelog.yaml.template
var lintChangelogTemplate string

func NewLintChangelogInput(p params.Params) input.Input {
	i := input.Input{
		Path:         params.RegenerableFileName(p, "lint-changelog.yaml"),
		TemplateBody: lintChangelogTemplate,
		TemplateDelims: input.InputTemplateDelims{
			Left:  "{{{{",
			Right: "}}}}",
		},
		TemplateData: map[string]interface{}{
			"Header": params.Header("#"),
		},
	}

	return i
}
