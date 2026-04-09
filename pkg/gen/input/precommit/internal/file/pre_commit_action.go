package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

//go:embed pre-commit-action.yaml.template
var createPreCommitActionTemplate string

func NewCreatePreCommitActionInput(p params.Params) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, ".github", "workflows", "zz_generated.pre-commit.yaml"),
		TemplateBody: createPreCommitActionTemplate,
		// Use non-default delimiters so Go's template engine does not interpret
		// the GitHub Actions ${{ }} expressions in the file content.
		TemplateDelims: input.InputTemplateDelims{Left: "[[", Right: "]]"},
		TemplateData: map[string]interface{}{
			"Language":     p.Language,
			"HasBash":      params.HasFlavor(p, "bash"),
			"HasMd":        params.HasFlavor(p, "md"),
			"HasHelmchart": params.HasFlavor(p, "helmchart"),
			"RepoName":     p.RepoName,
		},
	}
}
