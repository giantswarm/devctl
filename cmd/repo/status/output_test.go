package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
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
	for _, output := range []string{client.OutputText, client.OutputJSON} {
		for _, large := range []bool{false, true} {
			for _, remaining := range []int{0, 70} {
				t.Run(fmt.Sprintf("%s/large=%t/after=%d", output, large, remaining), func(t *testing.T) {
					payload := record
					if large {
						payload = strings.ReplaceAll(payload, "the run never reported", strings.Repeat("synthetic finding ", 800))
					}
					r, _, _ := newRunner(payload, output)
					caller := &outputCaller{payload: json.RawMessage(payload)}
					r.open = func(context.Context) (*client.Session, error) { return &client.Session{Caller: caller}, nil }
					r.stdout = &failingOutput{remaining: remaining, err: sentinel}
					require.ErrorIs(t, r.run(context.Background(), "my-service"), sentinel)
					require.Equal(t, 1, caller.calls)
				})
			}
		}
	}
}
