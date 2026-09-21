package auth

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/auth/login"
	"github.com/giantswarm/devctl/v8/cmd/auth/status"
)

const (
	name        = "auth"
	description = "Log in to GitHub and CircleCI for the agent-facing commands; tokens live in the OS keychain."
)

type Config struct {
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Stderr == nil {
		config.Stderr = os.Stderr
	}
	if config.Stdout == nil {
		config.Stdout = os.Stdout
	}

	var err error

	var loginCmd *cobra.Command
	{
		c := login.Config{
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		loginCmd, err = login.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var statusCmd *cobra.Command
	{
		c := status.Config{
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		statusCmd, err = status.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	c := &cobra.Command{
		Use:   name,
		Short: description,
		Long:  description,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	c.AddCommand(loginCmd)
	c.AddCommand(statusCmd)

	return c, nil
}
