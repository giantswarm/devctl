package checks

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "checks"
	shortDesc       = "Manage required status checks on the default branch"
	longDescription = `Manage required status checks on the default branch protection rule.

Use --update to add required checks. Checks already present are left as-is;
checks not in --checks are left unchanged. The branch must already have
protection configured.

Examples:
  devctl repo checks --update --checks semantic-pull-request,pre-commit giantswarm/my-repo`
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
		Use:   fmt.Sprintf("%s [flags] REPOSITORY", name),
		Short: shortDesc,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.ExactArgs(1),
	}

	f.Init(c)

	return c, nil
}
