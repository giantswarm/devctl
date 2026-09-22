// Package get is `devctl repo get`: one inventory record of
// giantswarm-repo-manager.
package get

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

const (
	name      = "get"
	shortDesc = "Print a repository's inventory record"
	longDesc  = `Print the inventory record giantswarm-repo-manager holds for one repository:
its declaration in the team file, the reality on GitHub (visibility, default
branch, last person commit, latest release and whether CircleCI built it),
the CircleCI facts, what its CircleCI configuration says (orb, arm64, China
push, signing), Renovate, the catalog and the mapping, the set-up state with
every step, the last reconciler run and the run awaited, and the findings
with their fix. The record is a cache: ` + "`devctl repo refresh`" + ` rebuilds it now.

Examples:
  devctl repo get my-service
  devctl repo get giantswarm/my-service --output json`
)

type Config struct {
	Logger *logrus.Logger
	Stderr io.Writer
	Stdout io.Writer
}

func New(config Config) (*cobra.Command, error) {
	return newCommand(config, name, shortDesc, longDesc, manager.ToolGetRepository)
}

// newCommand builds get or refresh: the same argument, the same record
// printed, another tool called.
func newCommand(config Config, name, short, long, tool string) (*cobra.Command, error) {
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
	r := &Runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, open: client.Open, tool: tool}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY", name),
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

// NewRefresh is `devctl repo refresh`, built here because it prints the same
// record.
func NewRefresh(config Config) (*cobra.Command, error) {
	return newCommand(config, "refresh", "Rebuild a repository's inventory record now and print it",
		`Rebuild one repository's inventory record now -- from GitHub, CircleCI's
statuses and the team files, the engine's checks run in read mode -- and print
it the way `+"`devctl repo get`"+` does. Writes the inventory cache only, nothing on
GitHub.

Examples:
  devctl repo refresh my-service
  devctl repo refresh giantswarm/my-service --output json`, manager.ToolRefreshRepository)
}

type flag struct {
	client.Flags
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
