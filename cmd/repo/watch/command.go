// Package watch is `devctl repo watch`: a new repository followed to
// readiness after its creation.
package watch

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
)

const (
	name      = "watch"
	shortDesc = "Follow a new repository to readiness after its creation"
	longDesc  = `Follow a repository created by ` + "`devctl repo create`" + ` (or the Repositories page,
or the agent) to readiness, and print each phase as it completes: created,
scaffolded, declared (the pull request is open), merged, set up (the
reconciler's run of the pull request has reported) and released (the first
release exists and its CircleCI statuses are green, complete and settled).

Once set up, the command sets the repository up on CircleCI as you, the one
step the reconciler cannot take: CircleCI has no service accounts. It does
what ` + "`devctl repo follow-circleci`" + ` does -- the GitHub App pipeline and its
all-pushes trigger, created with your CircleCI login (devctl auth login
--circleci-only, Write access), and a run of the default branch and of the
first release, which was tagged before the pipeline existed. When it ran that
release, the watch ends on that run, read with your CircleCI login: ready when
it is green. Otherwise it waits for the release as before. Nobody is load-bearing for the pipeline
afterwards. --circleci=false leaves it to someone's follow-circleci later.
A repository declared without CI (gen.ci.generate: false) has no release to
wait for: it is ready once set up.
giantswarm-repo-manager answers each call as soon as a phase completes or
after its own timeout; the command calls again until the repository is ready,
a phase fails, or --timeout runs out. The exit is 0 when ready, otherwise an
error naming the phase.

The watch ignores SIGHUP and reads no stdin, so it outlives its terminal.

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
	r := &runner{flag: f, logger: config.Logger, stderr: config.Stderr, stdout: config.Stdout, open: client.Open, setUpCircleCI: setUpCircleCI}

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
	CircleCI    bool
}

func (f *flag) Init(cmd *cobra.Command) {
	f.Flags.Init(cmd)
	cmd.Flags().IntVar(&f.PullRequest, "pull-request", 0, "The declaration pull request's number in giantswarm/github, as `devctl repo create` printed it. Required.")
	cmd.Flags().DurationVar(&f.Timeout, "timeout", defaultTimeout, "How long to follow before giving up.")
	cmd.Flags().BoolVar(&f.CircleCI, "circleci", true, "Set the repository up on its CircleCI GitHub App pipeline as you once it is set up.")
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

// defaultOwner is the owner of a repository named without one.
const defaultOwner = "giantswarm"

// setUpCircleCI is circleciapp.Setup as the person, without its own wait.
// When it ran release tags, the returned wait follows those runs with the
// person's CircleCI login: the watch's verdict on the release is theirs.
func setUpCircleCI(ctx context.Context, owner, repo string, progress func(format string, args ...any)) (tagWait, error) {
	gh, ci, err := circleciapp.OpenAsPerson(ctx, owner, func(w string) {
		if w != "" {
			progress("%s", w)
		}
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	report, err := circleciapp.Setup(ctx, ci, gh, owner, repo, circleciapp.Options{Progress: progress})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	var tags []circleciapp.Run
	for _, run := range report.Runs {
		if run.Ref.Tag != "" {
			tags = append(tags, run)
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	waiter := circleciapp.Waiter{CircleCI: ci, Sleep: sleep, Interval: tagPollInterval}
	return func(ctx context.Context) ([]circleciapp.Run, error) {
		for i := range tags {
			progress("waiting for pipeline %d (%s)", tags[i].Pipeline.Number, tags[i].Ref)
			outcome, err := waiter.Wait(ctx, tags[i].Pipeline.ID)
			if err != nil {
				return nil, microerror.Mask(err)
			}
			tags[i].Outcome = outcome
		}
		return tags, nil
	}, nil
}

// tagPollInterval is how often the release-tag runs are read.
const tagPollInterval = 10 * time.Second

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var invalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}
