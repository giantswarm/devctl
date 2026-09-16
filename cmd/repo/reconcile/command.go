package reconcile

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "reconcile"
	shortDesc       = "Run the repository set-up steps as the person"
	longDescription = `Run the repository set-up steps of the reconciler locally, with your
tokens: the way to repair a repository when the workflow is down and to
develop the engine against a real repository.

Every step is a check and a repair. Without --dry-run the drift is repaired
and the run converges in one go; a second run changes nothing. --dry-run
prints what a repair would change. What the engine cannot repair is
reported with the fix (a redirect on the declared name, a red first
release, a Renovate installation without the repository, ...).

The desired state is the repository's entry in a team file of
giantswarm/github (--team-file, the entry named after the repository) —
the scaffold is rendered from it when the repository is empty. A
repository without a declaration is reconciled from --team alone: the
settings, permissions, protection, CircleCI, Renovate and metadata steps
apply; the scaffold cannot be rendered.

The steps: create (only with --added), scaffold, settings, permissions,
protection (required checks on the reported-only rule: a context is
required once it has reported on the default branch or a recently merged
pull request, ghosts are removed), circleci (follow, setup workflows,
checkout key), webhooks, renovate (check only), codeowners (a pull
request), metadata, lifecycle, catalog, release. The CircleCI steps need a
token in $CIRCLECI_TOKEN and are skipped without one.

Examples:
  devctl repo reconcile --team-file repositories/team-bumblebee.yaml giantswarm/my-repo
  devctl repo reconcile --team-file repositories/team-bumblebee.yaml --dry-run --output json my-repo
  devctl repo reconcile --team team-bumblebee --steps settings,permissions,protection giantswarm/my-repo`
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
		Use:   fmt.Sprintf("%s [flags] REPOSITORY", name),
		Short: shortDesc,
		Long:  longDescription,
		RunE:  r.Run,
		Args:  cobra.ExactArgs(1),
	}

	f.Init(c)

	return c, nil
}
