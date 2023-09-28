package renovate

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "renovate"
	description     = "Enable (or disable) Renovate for the repository"
	longDescription = `This command adds the given repository to the list of repos that Renovate will have access to,
in order to allow for automatic dependency updates.

NOTE: This does not add or remove any renovate configuration to the repository.
For that, please check out:

  devctl gen renovate --help
`
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
		Use:     fmt.Sprintf("%s [--remove] REPOSITORY", name),
		Short:   description,
		Long:    longDescription,
		RunE:    r.Run,
		Args:    cobra.ExactArgs(1),
		Example: "devctl setup renovate --remove giantswarm/myrepo",
	}

	f.Init(c)

	return c, nil
}
