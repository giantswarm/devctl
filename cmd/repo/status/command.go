package status

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "status"
	shortDesc       = "Print a repository's set-up state"
	longDescription = `Print the set-up state of a declared repository: every set-up step of the
repository set-up engine with its verdict -- ok, drift (with the changes a
repair would make), reported (findings with their fix), skipped or failed
-- and whether the repository is set up as its team file declares. The
verdicts are the ones the Repositories page shows.

With a muster endpoint (--muster-endpoint or $MUSTER_ENDPOINT) the state
comes from giantswarm-repo-manager's inventory, as you; when the manager
cannot be reached the engine's checks run locally in read mode with your
GitHub token (and your CircleCI token, when $CIRCLECI_TOKEN is set, for the
CircleCI and release steps). Nothing is changed either way.

The repository must be declared in a team file of giantswarm/github; --team
names the file to read instead of searching them all.

Examples:
  devctl repo status my-service
  devctl repo status giantswarm/my-service --team bumblebee
  devctl repo status my-service --muster-endpoint https://muster.example.io/mcp --output json`
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
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY", name),
		Short: shortDesc,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.ExactArgs(1),
	}

	f.Init(c)

	return c, nil
}
