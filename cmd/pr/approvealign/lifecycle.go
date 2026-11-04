package approvealign

import (
	"context"
	"time"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

// waitForPR interrupts polling and post-write waits when the caller stops.
func waitForPR(ctx context.Context, ps *pr.PRStatus, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		ps.UpdateStatus("Cancelled")
		return false
	case <-timer.C:
		return true
	}
}
