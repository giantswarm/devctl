package project

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/project/internal/params"
	"github.com/giantswarm/microerror"
)

type Config struct {
	Name string
}

type Project struct {
	params params.Params
}

func New(config Config) (*Project, error) {
	if config.Name == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Name must not be empty", config)
	}

	c := &Project{
		params: params.Params{
			Name: config.Name,

			RootCommand: params.ParamsCommandTree{
				Name: "cmd",
				Subcommands: []params.ParamsCommandTree{
					{
						Name:        "example",
						Subcommands: []params.ParamsCommandTree{},
					},
					{
						Name: "test",
						Subcommands: []params.ParamsCommandTree{
							{
								Name:        "nested",
								Subcommands: []params.ParamsCommandTree{},
							},
						},
					},
				},
			},
		},
	}

	return c, nil
}

func (m *Project) ZZProject() input.Input {
	return file.NewZZProjectInput(m.params)
}
