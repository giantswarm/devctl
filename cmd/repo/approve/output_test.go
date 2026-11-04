package approve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type outputCaller struct {
	calls   int
	message bool
}

func (c *outputCaller) Call(context.Context, string, map[string]any) (json.RawMessage, error) {
	c.calls++
	if c.message {
		return json.RawMessage(`{"pullRequest":6179,"team":"team-example","member":true,"message":"Approved; waiting for checks."}`), nil
	}
	return json.RawMessage(`{"pullRequest":6179,"team":"team-example","member":true,"reviewUrl":"https://example.invalid/review/1","merged":true}`), nil
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

// A failed redirect must fail the command even though the remote approval
// has completed. It must never cause a second approval attempt.
func TestRunOutputFailure(t *testing.T) {
	for _, message := range []bool{false, true} {
		for _, format := range []string{client.OutputText, client.OutputJSON} {
			for _, remaining := range []int{0, 12, 70} {
				t.Run(fmt.Sprintf("%s/message_%t/after_%d_bytes", format, message, remaining), func(t *testing.T) {
					writeErr := errors.New("synthetic output failure")
					caller := &outputCaller{message: message}
					r := &runner{flag: &flag{Flags: client.Flags{Output: format}}, stdout: &failingOutput{remaining: remaining, err: writeErr},
						open: func(context.Context) (*client.Session, error) { return &client.Session{Caller: caller}, nil },
					}
					require.ErrorIs(t, r.run(context.Background(), "6179"), writeErr)
					require.Equal(t, 1, caller.calls)
				})
			}
		}
	}
}
