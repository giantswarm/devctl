package makefile

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	name             = "makefile"
	shortDescription = `Generates a Makefile for Golang projects.`
)

var longDescription = `Generates a Makefile for Golang projects.

There are different generation flavours:

` + gen.FlavourDescriptionList()

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

	f := new(flag)

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   name,
		Args:  cobra.NoArgs,
		Short: shortDescription,
		Long:  longDescription,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
