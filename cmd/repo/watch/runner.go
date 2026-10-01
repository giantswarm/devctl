package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
	open   client.Opener
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
		if w.Release != nil {
			line += fmt.Sprintf(" (release %s, %s)", w.Release.Tag, w.Release.URL)
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

var timeoutError = &microerror.Error{
	Kind: "timeoutError",
}

// IsTimeout asserts timeoutError: the repository was not ready in time.
func IsTimeout(err error) bool {
	return microerror.Cause(err) == timeoutError
}
