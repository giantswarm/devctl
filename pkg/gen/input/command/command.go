package command

import (
	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
	"github.com/giantswarm/microerror"
)

type Config struct {
	Dir  string
	Name string
}

type Command struct {
	params params.Params
}

func New(config Config) (*Command, error) {
	if config.Dir == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", config)
	}
	if config.Name == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Name must not be empty", config)
	}

	c := &Command{
		params: params.Params{
			Dir:  config.Dir,
			Name: config.Name,
		},
	}

	return c, nil
}

func (c *Command) Command() input.Input {
	return file.NewCommandInput(c.params)
}

func (c *Command) Error() input.Input {
	return file.NewErrorInput(c.params)
}

func (c *Command) Flags() input.Input {
	return file.NewFlagsInput(c.params)
}

func (c *Command) Runner() input.Input {
	return file.NewRunnerInput(c.params)
}
