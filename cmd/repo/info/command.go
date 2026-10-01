// Package info is `devctl repo info`: how giantswarm-repo-manager sees the
// call -- the caller, the identities, the inventory, the engine.
package info

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name      = "info"
	shortDesc = "Print how giantswarm-repo-manager sees this call"
	longDesc  = `Print giantswarm-repo-manager's version and how this call is authenticated:
the caller (the GitHub login your muster token acts as, through the App
giantswarm-repo-manager), whether your credential reaches the team files,
the identity and size of the inventory, where its CircleCI facts come from,
whether the team-review endpoint is configured, the engine's version and the
write modes. The first command to run when a repo command misbehaves.

Examples:
  devctl repo info
  devctl repo info --output json`
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
