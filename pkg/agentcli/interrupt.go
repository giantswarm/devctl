package agentcli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
)

// InterruptContext cancels a CLI wait on SIGINT or SIGTERM so it can report
// its document. The caller stops signal notification after reporting.
// Shared engines and server calls retain their caller's signal policy.
func InterruptContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

// Interrupted gives a canceled wait the timeout's exit code and identifies
// the interruption while retaining its last state. A completed outcome
// (success, failure or refusal) keeps its verdict.
func Interrupted(ctx context.Context, err error) error {
	if err == nil || !errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	// Login lookup/renewal can turn a canceled subprocess or request into
	// auth_required without retaining the context error in its chain.
	code := Exit(err)
	if code != ExitTimeout && code != ExitAuthRequired && !errors.Is(err, context.Canceled) {
		return err
	}
	return NewExitError(ExitTimeout, VerdictTimeout, "wait interrupted: %s", err)
}
