package checks

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel string
	}{
		{name: "case 0: canceled before execution", cancel: "before"},
		{name: "case 1: expired before execution", cancel: "expired"},
		{name: "case 2: canceled during protection lookup", cancel: "/branches/main/protection"},
		{name: "case 3: canceled during check discovery", cancel: "/pulls"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChecksFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			if tc.cancel == "before" {
				cancel()
			}
			if tc.cancel == "expired" {
				var release context.CancelFunc
				ctx, release = context.WithDeadline(ctx, time.Now().Add(-time.Minute))
				t.Cleanup(release)
			}
			f.before = func(req *http.Request) {
				if req.URL.Path == fixtureRepoPath+tc.cancel {
					cancel()
				}
			}

			output, err := f.execute(ctx, "--update", "--checks=external gate", "--output=json", "fixture/repository")
			require.Error(t, err)
			message := err.Error() + output
			if tc.cancel == "expired" {
				require.Contains(t, message, context.DeadlineExceeded.Error())
			} else {
				require.Contains(t, message, context.Canceled.Error())
			}
			require.Empty(t, f.writes, "a canceled invocation must not update protection")
			if tc.cancel == "before" || tc.cancel == "expired" {
				require.Empty(t, f.requests)
			}
		})
	}
}

func TestCommandDeadline(t *testing.T) {
	f := newChecksFixture(t)
	f.protection = nil
	f.ruleset = fixtureRuleset()
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	f.before = func(req *http.Request) {
		got, ok := req.Context().Deadline()
		require.True(t, ok, "each request must retain the command deadline")
		require.Equal(t, deadline, got)
	}
	_, err := f.execute(ctx, "--output=json", "fixture/repository")
	require.NoError(t, err)
	require.NotEmpty(t, f.requests)
	require.Empty(t, f.writes)
}
