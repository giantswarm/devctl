package reservation

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/extend"
	"github.com/giantswarm/devctl/v8/cmd/reservation/list"
	"github.com/giantswarm/devctl/v8/cmd/reservation/reap"
	"github.com/giantswarm/devctl/v8/cmd/reservation/release"
	"github.com/giantswarm/devctl/v8/cmd/reservation/reserve"
)

const (
	name            = "reservation"
	description     = "Commands for reserving a management cluster app for the dev builds of a branch."
	longDescription = description + `

A reservation points one app on a management cluster at the dev builds of a
branch for a limited time. The commands change the GitOps repository, and Flux
applies the change to the cluster.

  reserve  points an app at the dev builds of a branch.
  release  undoes a reservation, by cluster and app or by pull request.
  extend   resets the expiry of every reservation a pull request holds.
  list     shows the reservations on a management cluster.
  reap     releases every expired, closed or renamed reservation.

Every command except list takes --dry-run, which pushes nothing. Every command
takes --output table|json.`
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

	var extendCmd *cobra.Command
	{
		c := extend.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		var err error
		extendCmd, err = extend.New(c)
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
		Long:  longDescription,
		RunE:  r.Run,
	}

	f.Init(c)

	c.AddCommand(reserveCmd)
	c.AddCommand(releaseCmd)
	c.AddCommand(listCmd)
	c.AddCommand(reapCmd)
	c.AddCommand(extendCmd)

	return c, nil
}
