package command

import (
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	usage       = "command"
	description = `Generates Go CLI command code scaffolding.`
)

// TODO try to add test to check if everything is prefixed with "devctl gen command".
var examples = []string{
	`devctl gen command`,
}

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
	}

	c := &cobra.Command{
		Use:     usage,
		Example: "  " + strings.Join(examples, "  "),
		Short:   description,
		Long:    description,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
