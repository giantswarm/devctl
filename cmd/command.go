package cmd

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/cmd/completion"
	"github.com/giantswarm/devctl/cmd/gen"
	"github.com/giantswarm/devctl/cmd/release"
	"github.com/giantswarm/devctl/cmd/replace"
	"github.com/giantswarm/devctl/cmd/version"
	"github.com/giantswarm/devctl/pkg/project"
)

type Config struct {
	Logger micrologger.Logger
	Stderr io.Writer
	Stdout io.Writer

	BinaryName string
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

	var completionCmd *cobra.Command
	{
		c := completion.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		completionCmd, err = completion.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

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

	var versionCmd *cobra.Command
	{
		c := version.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
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
		Use:               project.Name(),
		Short:             project.Description(),
		Long:              project.Description(),
		RunE:              r.Run,
		PersistentPreRunE: r.PersistentPreRun,
		SilenceErrors:     true,
		SilenceUsage:      true,
	}

	f.Init(c)

	c.AddCommand(completionCmd)
	c.AddCommand(genCmd)
	c.AddCommand(releaseCmd)
	c.AddCommand(replaceCmd)
	c.AddCommand(versionCmd)

	return c, nil
}
