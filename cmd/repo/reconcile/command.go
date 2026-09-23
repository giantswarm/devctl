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
token in $CIRCLECI_TOKEN and are skipped without one. The catalog step
dispatches the catalog and mapping workflows of the catalog repository, which
needs an Actions permission there: --dispatch-token-envvar names a second
token for those two calls when the GitHub token's identity has none (a
workflow run's own token); every read, the repository lookup included, stays
with the GitHub token. The branch protection is the company baseline's:
administrators are bound too (enforce_admins), a branch need not be up to
date to merge (strict status checks off). With --devctl-app-id (the devctl
GitHub App's numeric id) the protection is a repository ruleset on the
default branch instead, "devctl: default branch", with the App and the
owning team as bypass actors for pull requests unless the entry declares
agentMerge: false, and
classic branch protection gives way to it in the same run; without the id
the step keeps classic protection and reports the missing id.

An entry the validator refuses is a result too: one step, entry, reported,
with a finding per problem naming the field to fix, and exit 0 — the
declaration is at fault, not the run. A flag or token error still exits 2.

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
