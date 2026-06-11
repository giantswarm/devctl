package reviewers

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"
)

const (
	name        = "reviewers"
	description = "Set the reviewers array in an existing Renovate config (renovate.json or renovate.json5)."
	example     = `  devctl gen renovate reviewers --reviewers team:team-rocket
  devctl gen renovate reviewers -r team:team-rocket -r team:team-honeybadger`
	longDescription = `Set the top-level "reviewers" array in an existing Renovate config.

The command edits renovate.json5 (preferred) or renovate.json in the current
directory in place, replacing the value of the "reviewers" key (or inserting it
if absent) while preserving all comments, quoting and formatting. It fails if no
Renovate config is found.`
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
		Use:     name,
		Short:   description,
		Long:    longDescription,
		Example: example,
		Args:    cobra.NoArgs,
		RunE:    r.Run,
	}

	f.Init(c)

	return c, nil
}
