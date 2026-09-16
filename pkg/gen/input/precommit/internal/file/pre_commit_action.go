package file

import (
	"embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

//go:generate go run ../../../update-template-sha.go pre-commit-action.yaml.template
//go:embed pre-commit-action.yaml.template
var createPreCommitActionTemplate string

//go:embed pre-commit-action.yaml.template*
var createPreCommitActionTemplateFiles embed.FS

var createPreCommitActionTemplateSha = input.TemplateSHA(createPreCommitActionTemplateFiles, "pre-commit-action.yaml.template")

func NewCreatePreCommitActionInput(p params.Params) input.Input {
	return input.Input{
		Path:         filepath.Join(p.Dir, ".github", "workflows", "zz_generated.pre-commit.yaml"),
		TemplateBody: createPreCommitActionTemplate,
		// Use non-default delimiters so Go's template engine does not interpret
		// the GitHub Actions ${{ }} expressions in the file content.
		TemplateDelims: input.InputTemplateDelims{Left: templateDelimLeft, Right: templateDelimRight},
		TemplateData: map[string]interface{}{
			templateKeyHeader: params.Header("#", createPreCommitActionTemplateSha),
			"Language":        p.Language,
			"HasBash":         params.HasFlavor(p, "bash"),
			"HasMd":           params.HasFlavor(p, "md"),
			"HasHelmchart":    params.HasFlavor(p, "helmchart"),
			"RepoName":        p.RepoName,
			"GoGenerate":      p.GoGenerate,
		},
	}
}
