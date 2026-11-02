package checks

import (
	"fmt"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	name            = "checks"
	shortDesc       = "Manage required status checks on the default branch"
	longDescription = `Manage the required status checks on the default branch's protection rule
with the protection step of the repository set-up engine.

A candidate is required once it has reported on the default branch's latest
non-tag commit or on the head of a recently merged pull request: a job that
never ran (no CI project, first run after it was added) cannot become a
required check nothing can satisfy. The CircleCI pipeline's branch-side
jobs are candidates: the generated workflows.yml and the repository's own
custom.yml, or a hand-kept config.yml when there is no generated pipeline,
read from the repository's .circleci, or from --circleci-dir when the
pipeline was just generated and is not pushed yet. A scheduled or
conditional workflow and an approval job are no candidates. A repository with a
tests/e2e directory has Heimdall's gatekeeper, 'Heimdall - PR Gatekeeper',
as one more candidate.

The command removes nothing it was not told to: a context already required
stays, whether it reported or not and whatever system reports it, unless
--remove names it or, with --circleci-dir, it is a 'ci/circleci:' context
whose job that pipeline does not have.

--checks names are required whatever reported, --checks-if-reported names
once they have reported, --remove names never. Without --update the drift
is printed and nothing changes. The branch's reviews, admin enforcement
and "up to date" (strict) setting are left as they are; the branch must
already be protected, by classic branch protection or by the ruleset
'devctl: default branch', of which only the required_status_checks rule
is written.

Examples:
  devctl repo checks giantswarm/my-repo
  devctl repo checks --update --checks 'semantic-pull-request / Validate PR title' giantswarm/my-repo
  devctl repo checks --update --remove semantic-pull-request giantswarm/my-repo
  devctl repo checks --update --checks-if-reported 'ci/circleci: build-chart,ci/circleci: execute-chart-tests' giantswarm/my-repo
  devctl repo checks --update --circleci-dir my-repo/.circleci --output json giantswarm/my-repo`
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
