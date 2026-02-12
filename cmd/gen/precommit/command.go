package precommit

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name        = "precommit"
	description = "Generates pre-commit configuration (.pre-commit-config.yaml)."
	example     = `  devctl gen precommit --language go
  devctl gen precommit --language go --flavors bash,helmchart
  devctl gen precommit --language generic --flavors md,bash`
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:     name,
		Short:   description,
		Long:    description,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
