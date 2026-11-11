package followcircleci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type runner struct {
	flag   *flag
	stdout io.Writer
	stderr io.Writer
	// The seams tests replace: the clients, the clock.
	open func(ctx context.Context, owner string, warn func(string)) (circleciapp.GitHub, circleciapp.CircleCI, error)
	// openSession reaches giantswarm-repo-manager, for --pending.
	openSession client.Opener
	sleep       func(ctx context.Context, d time.Duration) error
	interval    time.Duration
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	r.flag.WaitSet = cmd.Flags().Changed("wait") || cmd.Flags().Changed("timeout")
	if err := r.validate(args); err != nil {
		if r.flag.JSON() {
			doc := newDocument()
			return agentcli.Report(r.stdout, &doc, agentcli.VerdictGreen, agentcli.FlagError(command, err))
		}
		return microerror.Mask(err)
	}
	if r.flag.Pending {
		return r.runPending(ctx)
	}
	return microerror.Mask(r.run(ctx, args[0]))
}

// validate checks the arguments: one repository, or --pending.
func (r *runner) validate(args []string) error {
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	switch {
	case r.flag.Pending && len(args) > 0:
		return microerror.Maskf(invalidArgError, "--pending takes no repository")
	case r.flag.Pending && r.flag.Migrate:
		return microerror.Maskf(invalidArgError, "--migrate is for one repository, not --pending")
	case r.flag.Pending && r.flag.WaitSet:
		return microerror.Maskf(invalidArgError, "--pending does not wait for the runs: --wait and --timeout are for one repository")
	case !r.flag.Pending && len(args) != 1:
		return microerror.Maskf(invalidArgError, "expected [OWNER/]REPOSITORY, or --pending")
	case !r.flag.Pending && r.flag.JSON():
		return microerror.Maskf(invalidArgError, "--output json is for --pending")
	}
	return nil
}

func (r *runner) run(ctx context.Context, arg string) error {
	owner, repo, err := parse(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	warn := func(w string) {
		if w != "" {
			fmt.Fprintln(r.stderr, w)
		}
	}
	gh, ci, err := r.open(ctx, owner, warn)
	if err != nil {
		return microerror.Mask(err)
	}
	if r.flag.Wait {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.flag.Timeout)
		defer cancel()
	}
	report, err := circleciapp.Setup(ctx, ci, gh, owner, repo, circleciapp.Options{
		Migrate:  r.flag.Migrate,
		Wait:     r.flag.Wait,
		Waiter:   circleciapp.Waiter{CircleCI: ci, Sleep: r.sleep, Interval: r.interval},
		Progress: func(format string, args ...any) { fmt.Fprintf(r.stdout, format+"\n", args...) },
	})
	switch {
	case circleciclient.IsForbidden(err):
		return microerror.Mask(authstore.CircleCIWriteRequired("CircleCI refused to create the pipeline"))
	case errors.Is(err, context.DeadlineExceeded):
		return microerror.Maskf(timeoutError, "%s/%s: the runs did not settle within %s", owner, repo, r.flag.Timeout)
	case err != nil:
		return microerror.Mask(err)
	}
	if !report.Result.Changed() && len(report.Runs) == 0 {
		fmt.Fprintf(r.stdout, "%s/%s: set up already, nothing to do\n", owner, repo)
	}
	if !report.Green() {
		var red []string
		for _, run := range report.Runs {
			if run.Outcome != nil && !run.Outcome.Green {
				red = append(red, fmt.Sprintf("pipeline %d (%s)", run.Pipeline.Number, run.Ref))
			}
		}
		return microerror.Maskf(redError, "%s/%s is set up, and %s red: the pipeline is in place, the build failed", owner, repo, strings.Join(red, ", "))
	}
	return nil
}

// parse splits [OWNER/]REPOSITORY; the owner defaults to giantswarm.
func parse(arg string) (owner, repo string, err error) {
	arg = strings.TrimSpace(arg)
	owner, repo, found := strings.Cut(arg, "/")
	if !found {
		owner, repo = defaultOwner, arg
	}
	if owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", microerror.Maskf(invalidArgError, "expected [OWNER/]REPOSITORY, got %q", arg)
	}
	return owner, repo, nil
}
