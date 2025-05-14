package pr

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/cmd/pr/approvealign" // Import approvealign subcommand
)

const (
	name        = "pr"
	description = "Work with GitHub pull requests."
)

type Config struct {
	Logger *logrus.Logger // Keeping consistent with 'repo' command, though micrologger is used elsewhere
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

	var err error // Declare err

	var approveAlignCmd *cobra.Command
	{
		c := approvealign.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		approveAlignCmd, err = approvealign.New(c)
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

	c.AddCommand(approveAlignCmd) // Add the approvealign subcommand

	return c, nil
} 