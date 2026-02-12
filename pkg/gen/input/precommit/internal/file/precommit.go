package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

//go:embed pre-commit-config.yaml.template
var createPreCommitConfigTemplate string

func NewCreatePreCommitConfigInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, ".pre-commit-config.yaml"),
		TemplateBody: createPreCommitConfigTemplate,
		TemplateData: map[string]interface{}{
			"Language":     params.Language(p),
			"HasBash":      params.HasFlavor(p, "bash"),
			"HasMd":        params.HasFlavor(p, "md"),
			"HasHelmchart": params.HasFlavor(p, "helmchart"),
		},
	}

	return i
}
