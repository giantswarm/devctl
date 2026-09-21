package agentcli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"
)

// EnvTimeScale multiplies every sleep and timeout of an agent-facing command.
// Production runs at 1; the e2e suite runs at 0.001 so a five-minute wait
// takes 300 milliseconds. Timestamps are never scaled.
const EnvTimeScale = "DEVCTL_TIME_SCALE"

// Clock is the time source of a command: the current time, and sleeps and
// timeouts scaled by [EnvTimeScale].
type Clock struct {
	scale float64
	now   func() time.Time
}

// SystemClock is the wall clock at the scale of [EnvTimeScale] (1 when unset).
func SystemClock() (Clock, error) {
	scale := 1.0
	if s, ok := os.LookupEnv(EnvTimeScale); ok && s != "" {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f <= 0 {
			return Clock{}, fmt.Errorf("%s=%q: must be a positive number", EnvTimeScale, s)
		}
		scale = f
	}
	return NewClock(scale, time.Now), nil
}

// NewClock is a clock with an explicit scale and time source; now nil means
// the wall clock.
func NewClock(scale float64, now func() time.Time) Clock {
	if now == nil {
		now = time.Now
	}
	if scale <= 0 {
		scale = 1
	}
	return Clock{scale: scale, now: now}
}

// Now is the current time, unscaled.
func (c Clock) Now() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

// Scale is the factor applied to durations.
func (c Clock) Scale() float64 {
	if c.scale <= 0 {
		return 1
	}
	return c.scale
}

// Scaled is d at the clock's scale, never below one millisecond for a
// positive d.
func (c Clock) Scaled(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	scaled := time.Duration(float64(d) * c.Scale())
	if scaled < time.Millisecond {
		return time.Millisecond
	}
	return scaled
}

// Sleep waits for the scaled d or until ctx ends, whichever comes first, and
// returns ctx's error in the second case.
func (c Clock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(c.Scaled(d))
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Timeout derives a context that ends after the scaled d.
func (c Clock) Timeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.Scaled(d))
}
