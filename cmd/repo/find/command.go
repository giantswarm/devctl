package find

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name        = "find"
	description = `Find repositories based on very specific criteria

The --what flag allows to specify what search criteria should be used. When combining several critaria,
a repository will be returned when it's macthing at least one criteria (boolean OR).

Note: archived repositories are always excluded.

Criteria:

- README_OLD_CIRCLECI_BAGDE - A /README.md file is present, containing an outdated CircleCI badge.
- NO_CODEOWNERS - No /README-md file is present.
- DEFAULT_BRANCH_MASTER - The default branch is named 'master'.
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
		Use:   name,
		Short: description,
		Long:  description,
		RunE:  r.Run,
	}

	f.Init(c)

	return c, nil
}
