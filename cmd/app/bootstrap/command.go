package bootstrap

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name        = "bootstrap"
	description = "Bootstrap a new Giant Swarm app repository from an upstream Helm chart"
	example     = `  devctl app bootstrap \
    --name n8n \
    --upstream-repo https://github.com/n8n-io/n8n \
    --upstream-chart charts/n8n \
    --team planeteers \
    --sync-method vendir \
    --patch-method script`
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
