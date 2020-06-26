package mainpkg

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/mainpkg/internal/params"
	"github.com/giantswarm/microerror"
)

type Config struct {
	Name string
}

type Main struct {
	params params.Params
}

func New(config Config) (*Main, error) {
	if config.Name == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Name must not be empty", config)
	}

	c := &Main{
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

func (m *Main) ZZMain() input.Input {
	return file.NewZZMainInput(m.params)
}
