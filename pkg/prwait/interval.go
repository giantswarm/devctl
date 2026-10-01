package prwait

import (
	"time"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// The bounds of the poll interval at scale 1.
const (
	// MinInterval is the shortest a poll waits, however large the budget.
	MinInterval = 15 * time.Second
	// MaxInterval is the longest a poll waits, however small the budget.
	MaxInterval = 60 * time.Second
)

// requestsPerPoll is what one poll costs GitHub at most when nothing is
// cached: the pull request, the check runs, the statuses and the runs.
const requestsPerPoll = 4

// interval spreads the remaining budget over the time to its reset, so a
// wait never exhausts the engineer's hourly budget, and clamps the result to
// [MinInterval, MaxInterval]. An unknown budget polls at the floor, an
// exhausted one at the ceiling.
func interval(rate githubclient.RateLimit, now time.Time) time.Duration {
	if !rate.Known {
		return MinInterval
	}
	if rate.Remaining <= 0 {
		return MaxInterval
	}
	window := rate.Reset.Sub(now)
	if window <= 0 {
		return MinInterval
	}
	d := time.Duration(float64(window) * requestsPerPoll / float64(rate.Remaining))
	switch {
	case d < MinInterval:
		return MinInterval
	case d > MaxInterval:
		return MaxInterval
	}
	return d
}
