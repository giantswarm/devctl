//go:build devctl_e2e

package agentcli

import "time"

// Only the e2e binary replaces accelerated wall time with sleep-driven time.
// An explicit scale of 1 keeps scenarios that assert real-time bounds intact.
func systemClock(scale float64) Clock {
	if scale < 1 {
		return NewVirtualClock(time.Now())
	}
	return NewClock(scale, nil)
}
