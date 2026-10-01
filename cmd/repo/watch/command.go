// Package watch is `devctl repo watch`: a new repository followed to
// readiness after its creation.
package watch

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

const (
	name      = "watch"
	shortDesc = "Follow a new repository to readiness after its creation"
	longDesc  = `Follow a repository created by ` + "`devctl repo create`" + ` (or the Repositories page,
or the agent) to readiness, and print each phase as it completes: created,
scaffolded, declared (the pull request is open), merged, set up (the
reconciler's run of the pull request has reported) and released (the first
release exists and its CircleCI statuses are green, complete and settled).
giantswarm-repo-manager answers each call as soon as a phase completes or
after its own timeout; the command calls again until the repository is ready,
a phase fails, or --timeout runs out. The exit is 0 when ready, otherwise an
error naming the phase.

Examples:
  devctl repo watch my-service --pull-request 4711
  devctl repo watch giantswarm/my-service --pull-request 4711 --timeout 20m --output json`

	// callTimeout is what each call asks the manager to wait, under its 150 s
	// cap and the MCPServer's 180 s.
	callTimeout = 120
	// defaultTimeout bounds the whole watch: a repository takes three to four
	// minutes to its first release.
	defaultTimeout = 15 * time.Minute
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
	r := &runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, open: client.Open}

	c := &cobra.Command{
		Use:   fmt.Sprintf("%s [flags] [OWNER/]REPOSITORY", name),
		Short: shortDesc,
		Long:  longDesc,
		Args:  cobra.ExactArgs(1),
		RunE:  r.Run,
	}
	f.Init(c)
	return c, nil
}

type flag struct {
	client.Flags
	PullRequest int
	Timeout     time.Duration
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd)
	cmd.Flags().IntVar(&f.PullRequest, "pull-request", 0, "The declaration pull request's number in giantswarm/github, as `devctl repo create` printed it. Required.")
	cmd.Flags().DurationVar(&f.Timeout, "timeout", defaultTimeout, "How long to follow before giving up.")
}

func (f *flag) Validate() error {
	if err := f.Flags.Validate(); err != nil {
		return microerror.Mask(err)
	}
	if f.PullRequest <= 0 {
		return microerror.Maskf(client.InvalidFlagError, "--pull-request must name the declaration pull request")
	}
	if f.Timeout <= 0 {
		return microerror.Maskf(client.InvalidFlagError, "--timeout must be positive")
	}
	return nil
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
