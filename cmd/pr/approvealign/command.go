package approvealign

import (
	"io"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name        = "approvealign"
	shortCmd    = "approvealignfiles"   // Alias or a more descriptive name if needed
	longCmd     = "approve-align-files" // Used for cobra command registration
	description = "Approves 'Align files' PRs with passing status checks."
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}
	if config.Stderr == nil {
		// Default to os.Stderr if not provided, though it should be by the parent command
		config.Stderr = io.Discard // Or os.Stderr, depends on desired behavior
	}
	if config.Stdout == nil {
		// Default to os.Stdout if not provided
		config.Stdout = io.Discard // Or os.Stdout
	}

	f := &flag{}

	r := &runner{
		flag:   f,
		logger: config.Logger,
		stderr: config.Stderr,
		stdout: config.Stdout,
	}

	c := &cobra.Command{
		Use:     longCmd, // Use longCmd for `Use` to match user expectation for `devctl pr approve-align-files`
		Short:   description,
		Long:    description,
		Aliases: []string{name, shortCmd}, // Keep `approvealign` and `approvealignfiles` as aliases
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}