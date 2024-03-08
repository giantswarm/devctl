package file

import (
	_ "embed"
	"path/filepath"

	"github.com/giantswarm/devctl/v6/pkg/gen/input"
	"github.com/giantswarm/devctl/v6/pkg/gen/input/apptest/internal/params"
)

//go:embed config.yaml.template
var createConfigTemplate string

func NewCreateConfigInput(p params.Params) input.Input {
	i := input.Input{
		Path:         filepath.Join(p.Dir, "config.yaml"),
		TemplateBody: createConfigTemplate,
		TemplateData: map[string]interface{}{
			"appName":  params.AppName(p),
			"repoName": params.RepoName(p),
			"catalog":  params.Catalog(p),
		},
		SkipRegenCheck: true,
	}

	return i
}
