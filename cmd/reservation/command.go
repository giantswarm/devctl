package reservation

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/list"
	"github.com/giantswarm/devctl/v8/cmd/reservation/reap"
	"github.com/giantswarm/devctl/v8/cmd/reservation/release"
	"github.com/giantswarm/devctl/v8/cmd/reservation/reserve"
)

const (
	name        = "reservation"
	description = "Commands for reserving a management cluster app for the dev builds of a branch."
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

	var reserveCmd *cobra.Command
	{
		c := reserve.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		var err error
		reserveCmd, err = reserve.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var releaseCmd *cobra.Command
	{
		c := release.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		var err error
		releaseCmd, err = release.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var listCmd *cobra.Command
	{
		c := list.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		var err error
		listCmd, err = list.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var reapCmd *cobra.Command
	{
		c := reap.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		var err error
		reapCmd, err = reap.New(c)
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

	c.AddCommand(reserveCmd)
	c.AddCommand(releaseCmd)
	c.AddCommand(listCmd)
	c.AddCommand(reapCmd)

	return c, nil
}
