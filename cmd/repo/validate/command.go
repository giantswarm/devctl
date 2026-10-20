package validate

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
	name            = "validate"
	shortDesc       = "Validate team-file entries through giantswarm-repo-manager and print the dry run"
	longDescription = `Validate the entries of a giantswarm/github team file and print the dry run
as JSON: for every entry the rendered team-file entry (defaults applied), the
template it derives, the verdict of the GitHub name check and the problems,
each naming the field; for the change as a whole the guard notices. The
document's engine says who answered.

By default giantswarm-repo-manager answers, through your muster endpoint
with the muster token of the keychain (devctl auth login --muster-only): the
entries named with --entry go to its validate_repository as the declarations
being added to the team's file, so the dry run is the manager's own -- its
engine, the schema on giantswarm/github main, the names checked by its App,
the guard notices from your memberships as it reads them, align: true and
the visibility default written as it writes them -- the same answer the
Repositories page and the Repo Manager agent get. What the manager does not
do takes --local: existing mode, --schema, --author and --author-team,
another --owner.

--local runs devctl's own engine, and so does CI, where the keychain is
never read: --schema, else with a GitHub token -- the devctl GitHub App
login (devctl auth login --github-only), or a token in the environment that
overrides it (--github-token-envvar) -- the schema on giantswarm/github main
and the names checked on GitHub (an existing repository or a redirect from a
renamed one is taken), else the embedded schema and the names reported
unchecked.

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
The exit status is non-zero when an entry is refused.

Examples:
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry my-service
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry a --entry b
  devctl repo validate --team-file repositories/team-bumblebee.yaml --local
  devctl repo validate --team-file repositories/team-bumblebee.yaml --mode existing --entry marge --local
  devctl repo validate --team-file repositories/team-bumblebee.yaml --entry a --author octocat --author-team team-rocket --local
  devctl repo validate --team-file repositories/team-rocket.yaml --schema .github/repositories.schema.json --local`
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
		Use:   fmt.Sprintf("%s [flags]", name),
		Short: shortDesc,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.NoArgs,
	}

	f.Init(c)

	return c, nil
}
