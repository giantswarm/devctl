package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/circleciapp"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	open   client.Opener
	// setUpCircleCI sets the repository up on its CircleCI GitHub App
	// pipeline as the person, once the reconciler has set it up. wait is
	// non-nil when the set-up ran a release tag: it follows those runs to
	// their verdict.
	setUpCircleCI func(ctx context.Context, owner, repo string, progress func(format string, args ...any)) (wait tagWait, err error)
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}
	return microerror.Mask(r.run(context.Background(), args[0]))
}

// run calls the manager until the repository is ready, a phase fails or the
// timeout runs out, printing the phases as they complete; in JSON mode the
// last answer is printed whole.
func (r *runner) run(ctx context.Context, arg string) error {
	repository, err := client.Repository(arg)
	if err != nil {
		return microerror.Mask(err)
	}
	session, err := r.open(ctx)
	if err != nil {
		return microerror.Mask(err)
	}
	deadline := time.Now().Add(r.flag.Timeout)
	watchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	printed := map[string]bool{}
	circleCIDone := false
	lastReason := ""
	var last json.RawMessage
	for {
		remaining := time.Until(deadline)
		ask := callTimeout
		if remaining < time.Duration(ask)*time.Second {
			// Rounded up: a remaining 59.9 s asks for 60.
			ask = int((remaining + time.Second - 1) / time.Second)
			if ask < 1 {
				ask = 1
			}
		}
		callCtx, cancelCall := context.WithTimeout(watchCtx, time.Duration(ask+30)*time.Second)
		payload, err := session.Call(callCtx, manager.ToolWatchRepository, map[string]any{
			"repository": repository, "pullRequest": r.flag.PullRequest, "timeout": ask,
		})
		cancelCall()
		if err != nil {
			if errors.Is(watchCtx.Err(), context.DeadlineExceeded) {
				return microerror.Maskf(timeoutError, "%s is not ready after %s", repository, r.flag.Timeout)
			}
			return microerror.Mask(err)
		}
		last = payload
		watch, err := client.Decode[manager.Watch](manager.ToolWatchRepository, payload)
		if err != nil {
			return microerror.Mask(err)
		}
		if !r.flag.JSON() {
			r.printProgress(watch, printed, &lastReason)
		}
		if !circleCIDone && needsCircleCI(watch) && r.flag.CircleCI {
			circleCIDone = true
			wait, err := r.circleCI(watchCtx, repository)
			if err != nil {
				if r.flag.JSON() {
					_ = client.PrintJSON(r.stdout, last)
				}
				return microerror.Mask(err)
			}
			if wait != nil {
				// The set-up ran the release tag, which was cut before the
				// pipeline existed: its verdict is that run's, read with the
				// person's CircleCI login. The manager's released phase still
				// carries the reconciler's missed-tag-build from before.
				return r.waitTagRuns(watchCtx, repository, wait, last)
			}
		}
		switch {
		case watch.Failure != nil:
			if r.flag.JSON() {
				_ = client.PrintJSON(r.stdout, last)
			}
			return microerror.Maskf(failedError, "%s failed at %s: %s", repository, watch.Failure.Phase, watch.Failure.Reason)
		case watch.Ready:
			if r.flag.JSON() {
				return microerror.Mask(client.PrintJSON(r.stdout, last))
			}
			return nil
		}
		if time.Now().After(deadline) {
			if r.flag.JSON() {
				_ = client.PrintJSON(r.stdout, last)
			}
			return microerror.Maskf(timeoutError, "%s is not ready after %s: waiting for %s", repository, r.flag.Timeout, watch.Pending)
		}
	}
}

// needsCircleCI says the watch has reached the point where the person sets
// the CircleCI pipeline up: the reconciler set the repository up, it is
// declared with CI, and it is not ready yet (its release waits for CircleCI).
func needsCircleCI(w *manager.Watch) bool {
	if w.Ready || w.WithoutCI || (w.Failure != nil && w.Failure.Phase != phaseReleased) {
		return false
	}
	for _, p := range w.Phases {
		if p.Name == phaseSetUp {
			return true
		}
	}
	return false
}

// The phases the CircleCI step hangs on: setUp, the reconciler's run of the
// pull request reported; released, the first release's build.
const (
	phaseSetUp    = "setUp"
	phaseReleased = "released"
)

// circleCI runs setUpCircleCI for repository ([OWNER/]REPOSITORY, the owner
// giantswarm by default). A repository without a CircleCI configuration is
// left alone: its generated CI is GitHub Actions only.
func (r *runner) circleCI(ctx context.Context, repository string) (tagWait, error) {
	owner, repo, found := strings.Cut(repository, "/")
	if !found {
		owner, repo = defaultOwner, repository
	}
	wait, err := r.setUpCircleCI(ctx, owner, repo, r.circleCIProgress)
	switch {
	case circleciapp.IsNoConfig(err):
		return nil, nil
	case err != nil:
		return nil, microerror.Maskf(circleCIError, "%s/%s is set up, and its CircleCI pipeline is not: %v; log in with `devctl auth login --circleci-only` (grant Write) if that is the cause, then run `devctl repo follow-circleci %s/%s` and watch again, or watch with --circleci=false to leave the pipeline to someone else", owner, repo, err, owner, repo)
	}
	return wait, nil
}

func (r *runner) circleCIProgress(format string, args ...any) {
	if !r.flag.JSON() {
		fmt.Fprintf(r.stdout, "  circleci   "+format+"\n", args...)
	}
}

// tagWait follows the release-tag runs the CircleCI set-up started, as the
// person, until they settle.
type tagWait func(ctx context.Context) ([]circleciapp.Run, error)

// waitTagRuns ends the watch on the release-tag runs the set-up started:
// ready when every one is green, failed at released otherwise. In JSON mode
// the manager's last answer is printed, as on every other exit.
func (r *runner) waitTagRuns(ctx context.Context, repository string, wait tagWait, last json.RawMessage) error {
	runs, err := wait(ctx)
	if r.flag.JSON() {
		_ = client.PrintJSON(r.stdout, last)
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return microerror.Maskf(timeoutError, "%s is not ready after %s: the release build the CircleCI set-up started has not settled", repository, r.flag.Timeout)
	case err != nil:
		return microerror.Mask(err)
	}
	var red []string
	for _, run := range runs {
		if run.Outcome != nil && !run.Outcome.Green {
			red = append(red, fmt.Sprintf("pipeline %d (%s): %s", run.Pipeline.Number, run.Ref, strings.Join(run.Outcome.Failed, ", ")))
		}
	}
	if len(red) > 0 {
		if !r.flag.JSON() {
			fmt.Fprintf(r.stdout, "failed at %s: %s\n", phaseReleased, strings.Join(red, "; "))
		}
		return microerror.Maskf(failedError, "%s failed at %s: %s", repository, phaseReleased, strings.Join(red, "; "))
	}
	if !r.flag.JSON() {
		for _, run := range runs {
			fmt.Fprintf(r.stdout, "  %-10s pipeline %d (%s) green\n", phaseReleased, run.Pipeline.Number, run.Ref)
		}
		fmt.Fprintf(r.stdout, "ready: %s (the release build the CircleCI set-up started is green)\n", repository)
	}
	return nil
}

// printProgress prints the phases not printed yet, the pending phase's
// reason when it changed, and the closing line when ready.
func (r *runner) printProgress(w *manager.Watch, printed map[string]bool, lastReason *string) {
	for _, p := range w.Phases {
		if printed[p.Name] {
			continue
		}
		printed[p.Name] = true
		fmt.Fprintf(r.stdout, "  %-10s %s (+%ds)\n", p.Name, p.At.UTC().Format("2006-01-02T15:04:05Z"), p.Seconds)
	}
	switch {
	case w.Failure != nil:
		fmt.Fprintf(r.stdout, "failed at %s: %s\n", w.Failure.Phase, w.Failure.Reason)
	case w.Ready:
		line := "ready: " + w.Repository
		switch {
		case w.Release != nil:
			line += fmt.Sprintf(" (release %s, %s)", w.Release.Tag, w.Release.URL)
		case w.WithoutCI:
			line += " (no CI: set up, no release to wait for)"
		}
		fmt.Fprintln(r.stdout, line)
		for _, f := range w.Findings {
			fmt.Fprintf(r.stdout, "  %s: %s -- fix: %s\n", f.Kind, f.Message, f.Fix)
		}
	case w.Pending != "":
		reason := w.Pending
		if w.PendingReason != "" {
			reason += ": " + w.PendingReason
		}
		if reason != *lastReason {
			fmt.Fprintf(r.stdout, "waiting for %s\n", reason)
			*lastReason = reason
		}
	}
}

var failedError = &microerror.Error{
	Kind: "failedError",
}

// IsFailed asserts failedError: a phase of the creation failed.
func IsFailed(err error) bool {
	return microerror.Cause(err) == failedError
}

var circleCIError = &microerror.Error{
	Kind: "circleCIError",
}

var timeoutError = &microerror.Error{
	Kind: "timeoutError",
}

// IsTimeout asserts timeoutError: the repository was not ready in time.
func IsTimeout(err error) bool {
	return microerror.Cause(err) == timeoutError
}
