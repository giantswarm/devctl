package renovate

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/gen/renovate/reviewers"
)

const (
	name        = "renovate"
	description = "Generates Renovate config for go and docker dependencies (renovate.json5)."
	example     = `  devctl gen renovate
  devctl gen renovate --interval "after 9am on thursday"
  devctl gen renovate --language go --circleci-generated
  devctl gen renovate --language go --reviewers team:team-rocket
  devctl gen renovate --language go --deprecated`
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

	var reviewersCmd *cobra.Command
	{
		c := reviewers.Config{
			Logger: config.Logger,
			Stderr: config.Stderr,
			Stdout: config.Stdout,
		}

		reviewersCmd, err = reviewers.New(c)
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
		Use:     name,
		Short:   description,
		Long:    description,
		Example: example,
		RunE:    r.Run,
	}

	f.Init(c)

	c.AddCommand(reviewersCmd)

	return c, nil
}
