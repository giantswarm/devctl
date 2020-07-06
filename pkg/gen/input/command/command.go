package command

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/pkg/gen/input"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/file"
	"github.com/giantswarm/devctl/pkg/gen/input/command/internal/params"
)

type Config struct {
	Dir      string
	GoModule string
}

type Command struct {
	params params.Params
}

func New(config Config) (*Command, error) {
	if config.Dir == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Dir must not be empty", config)
	}
	if config.GoModule == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.GoModule must not be empty", config)
	}

	c := &Command{
		params: params.Params{
			Dir:      config.Dir,
			GoModule: config.GoModule,
		},
	}

	return c, nil
}

func (c *Command) Flags() input.Input {
	return file.NewFlagsInput(c.params)
}

func (c *Command) Meta() input.Input {
	return file.NewMetaInput(c.params)
}

func (c *Command) Run() input.Input {
	return file.NewRunInput(c.params)
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
