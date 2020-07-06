package project

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/params"
)

type Config struct {
	GoModule string
}

type Project struct {
	params params.Params
}

func New(config Config) (*Project, error) {
	if config.GoModule == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.GoModule must not be empty", config)
	}

	c := &Project{
		params: params.Params{
			GoModule: config.GoModule,
		},
	}

	return c, nil
}

func (m *Project) Project() input.Input {
	return file.NewProjectInput(m.params)
}

func (m *Project) ZZProject() input.Input {
	return file.NewZZProjectInput(m.params)
}
