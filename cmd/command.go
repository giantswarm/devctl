package cmd

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/cmd/gen"
	"github.com/giantswarm/devctl/cmd/release"
	"github.com/giantswarm/devctl/cmd/replace"
	"github.com/giantswarm/devctl/cmd/repo"
	"github.com/giantswarm/devctl/cmd/version"
)

const (
	name        = "devctl"
	description = "Command line development utility."
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer

	BinaryName string
	GitCommit  string
	Source     string
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

	if config.GitCommit == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.GitCommit must not be empty", config)
	}
	if config.Source == "" {
		return nil, microerror.Maskf(invalidConfigError, "%T.Source must not be empty", config)
	}

	var err error

	var genCmd *cobra.Command
	{
		c := gen.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		genCmd, err = gen.New(c)
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

		releaseCmd, err = release.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var replaceCmd *cobra.Command
	{
		c := replace.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		replaceCmd, err = replace.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var repoCmd *cobra.Command
	{
		c := repo.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		repoCmd, err = repo.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var versionCmd *cobra.Command
	{
		c := version.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,

			GitCommit: config.GitCommit,
			Source:    config.Source,
		}

		versionCmd, err = version.New(c)
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
		Use:          name,
		Short:        description,
		Long:         description,
		RunE:         r.Run,
		SilenceUsage: true,
	}

	f.Init(c)

	c.AddCommand(genCmd)
	c.AddCommand(versionCmd)
	c.AddCommand(releaseCmd)
	c.AddCommand(repoCmd)
	c.AddCommand(replaceCmd)

	return c, nil
}
