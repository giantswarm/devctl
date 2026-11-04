//go:build !devctl_e2e

package agentcli

func systemClock(scale float64) Clock {
	return NewClock(scale, nil)
}
