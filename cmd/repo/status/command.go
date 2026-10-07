package status

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name            = "status"
	shortDesc       = "Print a repository's set-up state from giantswarm-repo-manager"
	longDescription = `Print the set-up state of a declared repository as giantswarm-repo-manager's
inventory holds it: every set-up step of the repository set-up engine with its
verdict -- ok, drift (with the changes a repair would make), reported (findings
with their fix), skipped or failed -- whether the repository is set up as its
team file declares, the last reconciler run and the run awaited. The verdicts
are the ones the Repositories page shows.

The manager is reached through your muster endpoint with the muster token of
the keychain (` + "`devctl auth login --muster-only`" + `); the call is yours. Nothing
is changed. The local check with your own tokens is ` + "`devctl repo reconcile --dry-run`" + `.

Examples:
  devctl repo status my-service
  devctl repo status giantswarm/my-service --output json`
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
		open:   client.Open,
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
