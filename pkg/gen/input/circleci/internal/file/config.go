package file

import (
	_ "embed"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/params"
)

//go:embed config.yml.template
var configTemplate string

func NewConfigInput(p params.Params) input.Input {
	i := input.Input{
		Path:         ".circleci/config.yml",
		TemplateBody: configTemplate,
		TemplateData: map[string]interface{}{
			"RepoName":        p.RepoName,
			"Language":        p.Language,
			"HasDockerfile":   p.HasDockerfile,
			"HasApp":          p.HasApp,
			"BranchPublish":   p.BranchPublish,
			"ReleaseBinaries": p.ReleaseBinaries,
			"OrbVersion":      p.OrbVersion,
		},
	}

	return i
}
