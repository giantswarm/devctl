package create

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
	name            = "create"
	shortDesc       = "Declare a new repository: the team-file pull request the reconciler creates it from"
	longDescription = `Declare a new repository in giantswarm/github; the reconciler creates it.
The command renders the declaration as an entry of the team's file
(repositories/<team>.yaml), validates it through the engine -- the schema
on giantswarm/github main, the creation rules, the name free on GitHub --,
prints the dry run (the rendered entry, the template it derives, the name
check, the guard notices) and opens the declaration's pull request. Once it
merges, the reconciler creates the repository as the giantswarm-align-files
App (description and visibility from the declaration; without --visibility
it is private, the org's default, and the entry says so), pushes the
rendered scaffold as the one commit on main -- the scaffold's auto-release
workflow tags v0.1.0 from it -- and sets it up: settings, permissions,
protection, the catalog. The entry declares align: true, the repository's
opt-in to alignment. Nobody needs the organization's owner role.

CircleCI is the one step the reconciler cannot take: CircleCI has no service
accounts, so its GitHub App pipeline is created by a person. The command
prints the ` + "`devctl repo watch`" + ` that follows the repository to readiness;
it creates the pipeline with your CircleCI login once the reconciler has set
the repository up, and runs the first release on it. Nobody is load-bearing
for the pipeline afterwards.

A refusal of the declaration (a taken name, a wrong flavour, a schema
violation) ends the command before the pull request; the problems name the
fields. A rerun reports the pull request already open for the branch.
--dry-run asks giantswarm-repo-manager for its creation preview through
muster (devctl auth login --muster-only), prints its verdict and opens
nothing. --dry-run --local uses devctl's own engine with the schema on
giantswarm/github main, as a run without --dry-run does.
The notices say what review the pull request gets.

Without --dry-run, or with --dry-run --local, the token is the devctl GitHub
App login (devctl auth login), or the token in --github-token-envvar
($DEVCTL_GITHUB_TOKEN, $GITHUB_TOKEN or $OPSCTL_GITHUB_TOKEN) overriding it.
It reads the organisation's teams and, without --dry-run, writes the pull
request's branch to giantswarm/github.

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
