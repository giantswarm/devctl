package agentcli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// EnvTimeScale multiplies every sleep and timeout of an agent-facing command.
// Production runs at 1; the e2e suite runs at 0.001 so a five-minute wait
// takes 300 milliseconds. Timestamps are never scaled.
const EnvTimeScale = "DEVCTL_TIME_SCALE"

// Clock is the time source of a command: the current time, and sleeps and
// timeouts scaled by [EnvTimeScale].
type Clock struct {
	scale   float64
	now     func() time.Time
	virtual *virtualTime
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

// NewVirtualClock is a clock for tests whose time passes only in Sleep,
// starting at start: a sleep returns at once and moves Now on by d, and a
// Timeout ends when the sleeps reach it. A deadline never interrupts a call in
// flight, so the outcome of a wait does not depend on how fast the machine
// runs it.
func NewVirtualClock(start time.Time) Clock {
	return Clock{scale: 1, virtual: &virtualTime{now: start}}
}

// virtualTime is the shared state of a virtual clock and its copies.
type virtualTime struct {
	mu        sync.Mutex
	now       time.Time
	deadlines []virtualDeadline
}

type virtualDeadline struct {
	at     time.Time
	cancel context.CancelFunc
}

// advance moves the virtual time on by d and ends every Timeout it reaches.
func (v *virtualTime) advance(d time.Duration) {
	v.mu.Lock()
	v.now = v.now.Add(d)
	var due []context.CancelFunc
	pending := v.deadlines[:0]
	for _, deadline := range v.deadlines {
		if deadline.at.After(v.now) {
			pending = append(pending, deadline)
		} else {
			due = append(due, deadline.cancel)
		}
	}
	v.deadlines = pending
	v.mu.Unlock()
	for _, cancel := range due {
		cancel()
	}
}

// Now is the current time, unscaled.
func (c Clock) Now() time.Time {
	if c.virtual != nil {
		c.virtual.mu.Lock()
		defer c.virtual.mu.Unlock()
		return c.virtual.now
	}
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
	if c.virtual != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.virtual.advance(d)
		return ctx.Err()
	}
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
	if c.virtual != nil {
		ctx, cancel := context.WithCancel(ctx)
		c.virtual.mu.Lock()
		c.virtual.deadlines = append(c.virtual.deadlines, virtualDeadline{at: c.virtual.now.Add(d), cancel: cancel})
		c.virtual.mu.Unlock()
		c.virtual.advance(0)
		return ctx, cancel
	}
	return context.WithTimeout(ctx, c.Scaled(d))
}
