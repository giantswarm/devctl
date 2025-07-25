package llm

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name             = "llm"
	shortDescription = `Generates rules for LLM assistants.`
	longDescription  = `Generates rules for LLM assistants.

devctl gen llm --flavour app --language go

A base rule set is always generated. Some flavour- and language-specific rule sets are also supported.
`
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

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:   name,
		Short: shortDescription,
		Long:  longDescription,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
