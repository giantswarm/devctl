package setup

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/cmd/repo/setup/ciwebhooks"
	"github.com/giantswarm/devctl/v7/cmd/repo/setup/renovate"
)

const (
	name            = "setup"
	description     = `Configure GitHub repository`
	longDescription = `Configure GitHub repository with:

 - Settings
 - Permissions
 - Default branch protection rules`
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

	var renovateCmd *cobra.Command
	{
		c := renovate.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		renovateCmd, err = renovate.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var ciWebhooksCmd *cobra.Command
	{
		c := ciwebhooks.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		ciWebhooksCmd, err = ciwebhooks.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] REPOSITORY", name),
		Short: description,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.ExactArgs(1),
	}

	f.Init(c)

	c.AddCommand(renovateCmd)
	c.AddCommand(ciWebhooksCmd)

	return c, nil
}
