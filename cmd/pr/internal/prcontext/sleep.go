// Package prcontext provides interruptible waits for the bulk PR commands.
package prcontext

import (
	"context"
	"time"
)

// Sleep waits for d unless the command ends first.
func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
