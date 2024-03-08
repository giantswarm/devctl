package ci

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v6/cmd/ci/setup"
)

const (
	name             = "ci"
	shortDescription = `Configure CI/CD related settings.`
	longDescription  = `Configure CI/CD related settings.`
)

type Config struct {
	Logger *logrus.Logger
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

	var err error

	var setupCmd *cobra.Command
	{
		c := setup.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		setupCmd, err = setup.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		Logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   name,
		Short: shortDescription,
		Long:  longDescription,
		RunE:  r.Run,
	}

	f.Init(c)

	c.AddCommand(setupCmd)

	return c, nil
}
