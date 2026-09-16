package validate

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "validate"
	shortDesc       = "Validate team-file entries that create repositories and print the dry run"
	longDescription = `Validate the entries of a giantswarm/github team file that are about to
create repositories, and print the dry run as JSON: for every entry the
rendered team-file entry (defaults applied), the template it derives, the
verdict of the GitHub name check and the problems, each naming the field;
for the change as a whole the guard notices (the team's review is required
for an author outside the owning team and team-planeteers; a person reviews
more than three added entries).

The entries named with --entry are the ones being added; without --entry
every entry of the file is treated as added. The JSON is written to stdout
and nothing else is; log lines go to stderr. The exit status is non-zero
when an entry is refused. With a GitHub token the schema is read from
giantswarm/github main and the names are checked on GitHub (an existing
repository or a redirect from a renamed one is taken); without one the
embedded schema is used and the names are reported unchecked.

Examples:
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry my-service
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry a --entry b --author octocat --author-team team-rocket
  devctl repo validate --team-file repositories/team-rocket.yaml --schema .github/repositories.schema.json`
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
		Use:   fmt.Sprintf("%s [flags]", name),
		Short: shortDesc,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.NoArgs,
	}

	f.Init(c)

	return c, nil
}
