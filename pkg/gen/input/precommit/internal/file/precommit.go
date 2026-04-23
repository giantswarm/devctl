package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

//go:generate go run ../../../update-template-sha.go pre-commit-config.yaml.template
//go:embed pre-commit-config.yaml.template
var createPreCommitConfigTemplate string

//go:embed pre-commit-config.yaml.template.sha
var createPreCommitConfigTemplateSha string

func NewCreatePreCommitConfigInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, ".pre-commit-config.yaml"),
		TemplateBody: createPreCommitConfigTemplate,
		TemplateData: map[string]interface{}{
			"Header":       params.Header("#", createPreCommitConfigTemplateSha),
			"Language":     p.Language,
			"HasBash":      params.HasFlavor(p, "bash"),
			"HasMd":        params.HasFlavor(p, "md"),
			"HasHelmchart": params.HasFlavor(p, "helmchart"),
			"RepoName":     p.RepoName,
			"HelmCharts":   p.HelmCharts,
		},
	}

	return i
}
