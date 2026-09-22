// Package sweep is `devctl repo sweep`: the full inventory sweep, started now.
package sweep

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name      = "sweep"
	shortDesc = "Start the full inventory sweep of giantswarm-repo-manager now"
	longDesc  = `Start giantswarm-repo-manager's full inventory sweep now -- every repository's
record rebuilt from GitHub and the team files the way the schedule does it --
for a member of the teams that own the manager; a non-member is refused. The
sweep runs in the background: the answer says whether one was started (or was
already running) and what the last sweep found; ` + "`devctl repo list`" + ` shows the new
records when it is done. Nothing changes on GitHub.

Examples:
  devctl repo sweep
  devctl repo sweep --output json`
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
	r := &runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, open: client.Open}

	c := &cobra.Command{
		Use:   name,
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.NoArgs,
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

type flag struct {
	client.Flags
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
