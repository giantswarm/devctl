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

The rule is reported-only: a context is required once it has reported on the
default branch's latest non-tag commit or on the head of a recently merged
pull request, and a required context nothing reports any more is removed —
a job that never ran (no CI project, first run after it was added) cannot
become a required check nothing can satisfy, and a renamed or dropped job
cannot leave one behind. The release workflows, the dependency-graph
submission and the path-filtered workflows are never required. The
generated CircleCI pipeline's branch-side jobs are candidates: read from
the repository's .circleci, or from --circleci-dir when the pipeline was
just generated and is not pushed yet.

--checks names are required whatever reported, --checks-if-reported names
once they have reported, --remove names never. Without --update the drift
is printed and nothing changes. The branch's reviews, admin enforcement
and "up to date" (strict) setting are left as they are; the branch must
already have protection configured.

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
