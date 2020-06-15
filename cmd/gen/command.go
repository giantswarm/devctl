package gen

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/cmd/gen/ami"
	"github.com/giantswarm/devctl/cmd/gen/crud"
	"github.com/giantswarm/devctl/cmd/gen/kubeconfig"
	"github.com/giantswarm/devctl/cmd/gen/makefile"
	"github.com/giantswarm/devctl/cmd/gen/workflows"
)

const (
	name        = "gen"
	description = "Generate files."
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

	var amiCmd *cobra.Command
	{
		c := ami.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		amiCmd, err = ami.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var crudCmd *cobra.Command
	{
		c := crud.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		crudCmd, err = crud.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var kubeconfigCmd *cobra.Command
	{
		c := kubeconfig.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		kubeconfigCmd, err = kubeconfig.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var makefileCmd *cobra.Command
	{
		c := makefile.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		makefileCmd, err = makefile.New(c)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	var workflowsCmd *cobra.Command
	{
		c := workflows.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		workflowsCmd, err = workflows.New(c)
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

	c.AddCommand(amiCmd)
	c.AddCommand(crudCmd)
	c.AddCommand(kubeconfigCmd)
	c.AddCommand(makefileCmd)
	c.AddCommand(workflowsCmd)

	return c, nil
}
