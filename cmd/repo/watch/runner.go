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
	// pipeline as the person, once the reconciler has set it up.
	setUpCircleCI func(ctx context.Context, owner, repo string, progress func(format string, args ...any)) error
	// sleep pauses between calls the manager answers at once.
	sleep func(ctx context.Context, d time.Duration) error
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
	var circleCIAt time.Time
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
		if !circleCIDone && needsCircleCI(watch) && r.flag.CircleCI {
			circleCIDone = true
			circleCIAt = time.Now()
			if err := r.circleCI(watchCtx, repository); err != nil {
				return microerror.Mask(err)
			}
		}
		masked := maskReleasedFailure(watch, circleCIAt, time.Now())
		if !r.flag.JSON() {
			r.printProgress(watch, printed, &lastReason)
		}
		if masked {
			// The manager answers the stale failure at once until the tag
			// build reports; ask again after a pause, not in a tight loop.
			if err := r.sleep(watchCtx, releasedRecheck); err != nil {
				return microerror.Maskf(timeoutError, "%s is not ready after %s: waiting for the tag build the CircleCI set-up started", repository, r.flag.Timeout)
			}
			continue
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

// releasedGrace is how long after the CircleCI set-up a failed released
// phase is taken for the stale verdict it is: the first tag was cut before
// the pipeline existed, so the reconciler's run reported missed-tag-build,
// and the manager repeats that until the run the set-up started reports on
// the tag. releasedRecheck is the pause between the calls meanwhile.
const (
	releasedGrace   = 10 * time.Minute
	releasedRecheck = 15 * time.Second
)

// maskReleasedFailure turns a failed released phase into a pending one while
// the CircleCI set-up of this watch (at) is younger than releasedGrace, and
// says so. needsCircleCI lets the set-up run on exactly that answer: it is
// the normal answer for a repository whose first tag came before its
// pipeline.
func maskReleasedFailure(w *manager.Watch, at, now time.Time) bool {
	if at.IsZero() || w.Failure == nil || w.Failure.Phase != phaseReleased || now.Sub(at) > releasedGrace {
		return false
	}
	w.Pending = phaseReleased
	w.PendingReason = "the tag build the CircleCI set-up started has not reported yet (the manager still reports: " + w.Failure.Reason + ")"
	w.Failure = nil
	return true
}

// circleCI runs setUpCircleCI for repository ([OWNER/]REPOSITORY, the owner
// giantswarm by default). A repository without a CircleCI configuration is
// left alone: its generated CI is GitHub Actions only.
func (r *runner) circleCI(ctx context.Context, repository string) error {
	owner, repo, found := strings.Cut(repository, "/")
	if !found {
		owner, repo = defaultOwner, repository
	}
	progress := func(format string, args ...any) {
		if !r.flag.JSON() {
			fmt.Fprintf(r.stdout, "  circleci   "+format+"\n", args...)
		}
	}
	err := r.setUpCircleCI(ctx, owner, repo, progress)
	switch {
	case circleciapp.IsNoConfig(err):
		return nil
	case err != nil:
		return microerror.Maskf(circleCIError, "%s/%s is set up, and its CircleCI pipeline is not: %v; fix it, then run `devctl repo follow-circleci %s/%s` and watch again", owner, repo, err, owner, repo)
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
