package version

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v2/cmd/version/check"
	"github.com/giantswarm/devctl/v2/cmd/version/update"
)

const (
	name        = "version"
	description = "Prints version information."
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

	var err error

	var checkCmd *cobra.Command
	{
		c := check.Config(config)

		checkCmd, err = check.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var updateCmd *cobra.Command
	{
		c := update.Config(config)

		updateCmd, err = update.New(c)
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
		Use:   name,
		Short: description,
		Long:  description,
		RunE:  r.Run,
	}

	f.Init(c)

	c.AddCommand(checkCmd)
	c.AddCommand(updateCmd)

	return c, nil
}
