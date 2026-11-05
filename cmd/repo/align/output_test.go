package align

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type outputCaller struct {
	payload json.RawMessage
	calls   int
}

func (c *outputCaller) Call(context.Context, string, map[string]any) (json.RawMessage, error) {
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

func TestRunOutputErrors(t *testing.T) {
	sentinel := errors.New("synthetic output full")
	for _, dryRun := range []bool{true, false} {
		for _, output := range []string{client.OutputText, client.OutputJSON} {
			for _, large := range []bool{false, true} {
				for _, remaining := range []int{0, 70} {
					t.Run(fmt.Sprintf("dry=%t/%s/large=%t/after=%d", dryRun, output, large, remaining), func(t *testing.T) {
						detail := "synthetic detail"
						if large {
							detail = strings.Repeat(detail, 800)
						}
						dispatch := &manager.Dispatch{
							Mode: "opt-in", Declared: true, Team: "team", Then: detail,
							OptIn: &manager.OptIn{
								Plan: manager.Plan{Repository: "example/repo", Team: "team", Accepted: true, Entry: detail},
							},
						}
						if !dryRun {
							dispatch.OptIn.Committed = &manager.Committed{PullRequest: &manager.PullRequest{URL: "https://github.com/example/inventory/pull/1", Title: detail}}
						}
						payload, err := json.Marshal(dispatch)
						require.NoError(t, err)
						caller := &outputCaller{payload: payload}
						r := &runner{
							flag:   &flag{Flags: client.Flags{Output: output}, DryRun: dryRun},
							stdout: &failingOutput{remaining: remaining, err: sentinel},
							open:   func(context.Context) (*client.Session, error) { return &client.Session{Caller: caller}, nil },
						}
						require.ErrorIs(t, r.run(context.Background(), "repo"), sentinel)
						require.Equal(t, 1, caller.calls)
					})
				}
			}
		}
	}
}
