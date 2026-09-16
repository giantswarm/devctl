package create

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "create"
	shortDesc       = "Declare a new repository in its team file and open the pull request"
	longDescription = `Create a repository the way every client of the repository set-up engine
does: declare it. The command renders the declaration as an entry of the
team's file in giantswarm/github (repositories/<team>.yaml), validates it
through the engine -- the schema on giantswarm/github main, the creation
rules, the name checked on GitHub -- prints the dry run (the rendered entry,
the template it derives, the name check, the guard notices) and opens the
pull request as you, with your own GitHub login. It never creates the
repository or touches GitHub settings: the reconciler does that from the
merged entry.

A taken name, a wrong flavour or any other refusal ends the command before
a pull request exists. The notices tell you beforehand what review the pull
request gets: the machine approves a creation-only change by a member of
the owning team or team-planeteers; anyone else's keeps the team's review.

The token is $GITHUB_TOKEN (--github-token-envvar) or, without one, the
login of your gh CLI. It needs to read the organisation's teams (read:org)
for the notices and to write giantswarm/github for the pull request.

Examples:
  devctl repo create --team bumblebee --name my-service --component-type service --flavour app --language go --description "What it does"
  devctl repo create --team team-rocket --name my-chart --component-type service --flavour app --language generic --visibility public --dry-run
  devctl repo create --team bumblebee --name customer-configs --component-type customer --flavour customer --language generic --output json`
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
