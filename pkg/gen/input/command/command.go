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

func (c *Command) ZZCommand() input.Input {
	return file.NewZZCommandInput(c.params)
}

func (c *Command) ZZError() input.Input {
	return file.NewZZErrorInput(c.params)
}

func (c *Command) ZZFlags() input.Input {
	return file.NewZZFlagsInput(c.params)
}

func (c *Command) ZZRunner() input.Input {
	return file.NewZZRunnerInput(c.params)
}
