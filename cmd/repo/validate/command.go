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
	shortDesc       = "Validate team-file entries and print the dry run"
	longDescription = `Validate the entries of a giantswarm/github team file and print the dry run
as JSON: for every entry the rendered team-file entry (defaults applied), the
template it derives, the verdict of the GitHub name check and the problems,
each naming the field; for the change as a whole the guard notices.

Two modes, --mode create|existing. In create mode the entries are being
added and the reconciler creates their repositories: on top of the schema
the creation rules apply (gen.flavours and gen.language are set, a template
exists for them, generated CI has a job, the name follows the convention
and is free on GitHub), and the guard notices say what review the change
gets (the team's review is required for an author outside the owning team
and team-planeteers; a person reviews more than three added entries). In
existing mode the entries declare repositories that exist and the schema
alone decides; the name check's verdict is reported and never refuses — a
missing repository is the reconciler's finding. The default is create when
--entry names the entries being added, existing when the whole file is
validated: it is on main already.

The JSON is written to stdout and nothing else is; log lines go to stderr.
The exit status is non-zero when an entry is refused. With a GitHub token
-- the devctl GitHub App login (devctl auth login --github-only), or a
token in the environment that overrides it (--github-token-envvar) -- the
schema is read from giantswarm/github main and the names are checked on
GitHub (an existing repository or a redirect from a renamed one is taken);
without one the embedded schema is used and the names are reported
unchecked. With CI set the keychain is not read.

Examples:
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry my-service
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry a --entry b --author octocat --author-team team-rocket
  devctl repo validate --team-file repositories/team-bumblebee.yaml
  devctl repo validate --team-file repositories/team-bumblebee.yaml --mode existing --entry marge
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
