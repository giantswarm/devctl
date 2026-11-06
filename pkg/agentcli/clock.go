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
// Production runs at 1. In normal builds, a scale of 0.001 makes a
// five-minute sleep take 300 milliseconds. Timestamps are never scaled.
const EnvTimeScale = "DEVCTL_TIME_SCALE"

// Clock is the time source of a command: the current time, and sleeps and
// timeouts scaled by [EnvTimeScale].
type Clock struct {
	scale   float64
	now     func() time.Time
	virtual *virtualTime
}

// SystemClock is the wall clock at the scale of [EnvTimeScale] (1 when unset).
// The devctl_e2e build uses sleep-driven virtual time for scales below 1.
func SystemClock() (Clock, error) {
	scale := 1.0
	if s, ok := os.LookupEnv(EnvTimeScale); ok && s != "" {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f <= 0 {
			return Clock{}, fmt.Errorf("%s=%q: must be a positive number", EnvTimeScale, s)
		}
		scale = f
	}
	return systemClock(scale), nil
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

// A virtual deadline travels with the context without advertising a wall-clock
// deadline to net/http. Only Sleep advances it; a request in flight cannot.
type virtualDeadlineKey struct{}

type virtualContextDeadline struct {
	clock *virtualTime
	at    time.Time
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

// MinScaledTimeout is the real-time floor of a timeout on a clock scaled
// below 1: a one-minute wait at scale 0.001 would otherwise leave 60ms, which
// a loaded machine can spend before the first poll returns. Sleeps stay
// scaled, so a wait still polls in milliseconds.
const MinScaledTimeout = 5 * time.Second

// ScaledTimeout is the real duration of a timeout of d: d at the clock's
// scale, but at a scale below 1 never less than MinScaledTimeout, nor more
// than d itself.
func (c Clock) ScaledTimeout(d time.Duration) time.Duration {
	scaled := c.Scaled(d)
	if floor := min(d, MinScaledTimeout); scaled < floor {
		return floor
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

// Timeout derives a context that ends after [Clock.ScaledTimeout] of d.
func (c Clock) Timeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if c.virtual != nil {
		at := c.Now().Add(d)
		if parent, ok := ctx.Value(virtualDeadlineKey{}).(virtualContextDeadline); ok && parent.clock == c.virtual && parent.at.Before(at) {
			at = parent.at
		}
		ctx = context.WithValue(ctx, virtualDeadlineKey{}, virtualContextDeadline{clock: c.virtual, at: at})
		ctx, cancel := context.WithCancel(ctx)
		c.virtual.mu.Lock()
		c.virtual.deadlines = append(c.virtual.deadlines, virtualDeadline{at: at, cancel: cancel})
		c.virtual.mu.Unlock()
		c.virtual.advance(0)
		return ctx, cancel
	}
	return context.WithTimeout(ctx, c.ScaledTimeout(d))
}

// deadline is a context's deadline on the clock's unscaled time line. Virtual
// deadlines stay separate from real parent deadlines, which still cancel work.
func (c Clock) deadline(ctx context.Context) (time.Time, bool) {
	if c.virtual != nil {
		var at time.Time
		if virtual, ok := ctx.Value(virtualDeadlineKey{}).(virtualContextDeadline); ok && virtual.clock == c.virtual {
			at = virtual.at
		}
		if real, ok := ctx.Deadline(); ok {
			parent := c.Now().Add(time.Until(real))
			if at.IsZero() || parent.Before(at) {
				at = parent
			}
		}
		return at, !at.IsZero()
	}
	d, ok := ctx.Deadline()
	if !ok {
		return time.Time{}, false
	}
	return c.Now().Add(time.Duration(float64(time.Until(d)) / c.Scale())), true
}
