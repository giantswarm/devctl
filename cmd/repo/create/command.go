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
	shortDesc       = "Create a repository as yourself: create, scaffold, then the declaration's pull request"
	longDescription = `Create a repository the way every client of the repository set-up engine
does, as yourself. The command renders the declaration as an entry of the
team's file in giantswarm/github (repositories/<team>.yaml), validates it
through the engine -- the schema on giantswarm/github main, the creation
rules, the name free on GitHub -- and prints the dry run (the rendered
entry, the template it derives, the name check, the guard notices). Then,
with your own GitHub login, it creates the repository (description and
visibility from the declaration), pushes the rendered scaffold as the one
commit on main -- the scaffold's auto-release workflow tags v0.1.0 from it
-- and opens the declaration's pull request last, so the change under
review declares a repository that exists and is yours. Everything else --
settings, permissions, protection, CircleCI, the catalog -- the reconciler
applies from the merged entry.

Only an organization owner may create a repository in giantswarm: the
organization does not let members create them. Your role is read before
anything is written; anyone else is refused with the way out -- ask an
owner, or use the Dev Portal, which creates as you too and needs the same
role.

A refusal of the declaration (a taken name, a wrong flavour, a schema
violation) ends the command before anything exists; the problems name the
fields. A run interrupted after the creation resumes: a repository of that
name that you administer is yours to continue -- the scaffold is pushed
when missing, the pull request opened when none is open for its branch.
--dry-run prints the dry run and the plan (create, scaffold) and writes
nothing. The notices tell you beforehand what review the pull request
gets: the machine approves a creation-only change by a member of the
owning team or team-planeteers; anyone else's keeps the team's review.

The token is $GITHUB_TOKEN (--github-token-envvar) or, without one, the
login of your gh CLI. It creates the repository and pushes the scaffold
(repo, and workflow for the scaffold's GitHub Actions workflows), reads
the organisation's teams and your role in it (read:org) and writes
giantswarm/github for the pull request.

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
