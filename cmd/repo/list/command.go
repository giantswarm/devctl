// Package list is `devctl repo list`: the inventory of the org's
// repositories as giantswarm-repo-manager holds it, scoped and filtered as
// on the Repositories page.
package list

import (
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name      = "list"
	shortDesc = "List the org's repositories from the inventory"
	longDesc  = `List the repositories of the org from giantswarm-repo-manager's inventory: one
row per repository with its team, lifecycle, Renovate state, set-up state,
last person commit and findings, plus the last sweep. The scope is yours by
default -- the repositories of the teams you belong to on GitHub, read as you
-- and the filters are the Repositories page's.

Examples:
  devctl repo list
  devctl repo list --scope unassigned --inactive-days 365
  devctl repo list --scope all --team team-bumblebee --renovate inactive
  devctl repo list --scope all --finding declared-but-gone --output json`
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

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
