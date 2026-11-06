package followcircleci

import (
	"bytes"
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGuard_APanicDoesNotEscape(t *testing.T) {
	require.NotPanics(t, func() {
		guard(t.Context(), time.Minute, func(context.Context) { panic("boom") })
	})
}

func TestGuard_GivesUpOnAPassThatIgnoresItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		start := time.Now()

		// A keychain prompt nobody answers: it ignores the context.
		guard(t.Context(), 20*time.Second, func(context.Context) { <-release })

		require.Equal(t, 20*time.Second+guardGrace, time.Since(start))
	})
}

func TestGuard_ReturnsWhenInterrupted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		ctx, cancel := context.WithCancel(t.Context())
		start := time.Now()
		time.AfterFunc(time.Second, cancel)

		guard(ctx, time.Minute, func(context.Context) { <-release })

		require.Equal(t, time.Second, time.Since(start))
	})
}

func TestAuto_NeverPanicsWithoutAHomeWhenTurnedOff(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(OptOutEnv, "1")
	require.NotPanics(t, func() { Auto(commands()["pr wait"], &bytes.Buffer{}) })
}

func TestAuto_NeverPanicsWithoutAHomeAfterASkippedCommand(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	require.NotPanics(t, func() { Auto(commands()["version"], &bytes.Buffer{}) })
}

func TestConfigDir_NoHomeIsAnError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := configDir()

	require.Error(t, err)
}

func TestConfigDir_FollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	dir, err := configDir()

	require.NoError(t, err)
	require.Equal(t, "/xdg/devctl", dir)
}
