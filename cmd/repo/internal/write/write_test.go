package write

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type outputCaller struct {
	payload json.RawMessage
	calls   int
}

func (c *outputCaller) Call(_ context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
	c.calls++
	return c.payload, nil
}

type failingOutput struct {
	remaining int
	err       error
}

func (w *failingOutput) Write(p []byte) (int, error) {
	if len(p) <= w.remaining {
		w.remaining -= len(p)
		return len(p), nil
	}
	n := w.remaining
	w.remaining = 0
	return n, w.err
}

func writeAnswer(t *testing.T, dryRun, large bool) json.RawMessage {
	t.Helper()
	detail := "synthetic detail"
	if large {
		detail = strings.Repeat("synthetic detail ", 800)
	}
	var answer any = &manager.Committed{
		PullRequest: &manager.PullRequest{URL: "https://github.com/example/inventory/pull/1", Title: detail},
		Ask:         &manager.Delivery{Team: "team", Channel: "CTEAM", Delivered: true},
		Notice:      &manager.Delivery{Team: "other", Error: detail},
		PendingRun:  &manager.PendingRun{By: detail, DispatchedAt: time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)},
	}
	if dryRun {
		answer = &manager.Plan{
			Repository: "example/repo", Team: "team", Accepted: true, Entry: detail,
			PullRequest: manager.PlannedPullRequest{Title: "change", Branch: "branch", As: "alice"},
			Ask:         &manager.PlannedMessage{Team: "team", Text: detail, Deliverable: true},
			Notice:      &manager.PlannedMessage{Team: "other", Reason: detail},
		}
	}
	payload, err := json.Marshal(answer)
	require.NoError(t, err)
	return payload
}

// The shared write path must return sink errors even after a committed
// response; losing its receipt must never send the write again.
func TestRunOutputErrors(t *testing.T) {
	sentinel := errors.New("synthetic output full")
	for _, dryRun := range []bool{true, false} {
		for _, large := range []bool{false, true} {
			for _, output := range []string{client.OutputText, client.OutputJSON} {
				for _, remaining := range []int{0, 70} {
					t.Run(fmt.Sprintf("dry=%t/large=%t/%s/after=%d", dryRun, large, output, remaining), func(t *testing.T) {
						caller := &outputCaller{payload: writeAnswer(t, dryRun, large)}
						err := Run(context.Background(), &client.Session{Caller: caller}, &failingOutput{remaining: remaining, err: sentinel},
							&Flags{Flags: client.Flags{Output: output}, DryRun: dryRun}, manager.ToolUpdateRepository, map[string]any{"repository": "repo"})
						require.ErrorIs(t, err, sentinel)
						require.Equal(t, 1, caller.calls)
					})
				}
			}
		}
	}
}

func TestRunOutputShortWrite(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		caller := &outputCaller{payload: writeAnswer(t, dryRun, false)}
		err := Run(context.Background(), &client.Session{Caller: caller}, &failingOutput{remaining: 5},
			&Flags{Flags: client.Flags{Output: client.OutputText}, DryRun: dryRun}, manager.ToolAdoptRepository, map[string]any{})
		require.ErrorIs(t, err, io.ErrShortWrite)
		require.Equal(t, 1, caller.calls)
	}
}

func TestRunOutputSuccess(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		caller := &outputCaller{payload: writeAnswer(t, dryRun, true)}
		var out bytes.Buffer
		err := Run(context.Background(), &client.Session{Caller: caller}, &out,
			&Flags{Flags: client.Flags{Output: client.OutputText}, DryRun: dryRun}, manager.ToolTransferRepository, map[string]any{})
		require.NoError(t, err)
		require.Equal(t, 1, caller.calls)
		if dryRun {
			require.Contains(t, out.String(), "dry run: nothing written\nexample/repo in team: accepted")
			require.Contains(t, out.String(), "ask: to team")
			require.Contains(t, out.String(), "notice: to other, not deliverable:")
		} else {
			require.Contains(t, out.String(), "pull request: https://github.com/example/inventory/pull/1")
			require.Contains(t, out.String(), "ask: delivered to team in #CTEAM")
			require.Contains(t, out.String(), "notice: not delivered")
			require.Contains(t, out.String(), "pending run: expected since")
		}
		require.True(t, strings.HasSuffix(out.String(), "\n"))
	}
}
