package info

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type contextCaller func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f contextCaller) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

// ExecuteContext's deadline and values reach both authentication and the
// manager; the call's child context is released when the command finishes.
func TestRunUsesCommandContext(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), contextKey{}, "caller"), 30*time.Second)
	defer cancel()
	var openedCtx, calledCtx context.Context
	r := &runner{
		flag:   &flag{},
		stdout: io.Discard,
		open: func(ctx context.Context) (*client.Session, error) {
			openedCtx = ctx
			return &client.Session{Caller: contextCaller(func(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
				calledCtx = ctx
				return json.RawMessage(`{}`), nil
			})}, nil
		},
	}
	cmd := &cobra.Command{Use: name, RunE: r.Run}
	r.flag.Init(cmd)
	cmd.SetArgs([]string{})

	require.NoError(t, cmd.ExecuteContext(ctx))
	require.Same(t, ctx, openedCtx)
	require.NotNil(t, calledCtx)
	require.Equal(t, "caller", calledCtx.Value(contextKey{}))
	wantDeadline, _ := ctx.Deadline()
	gotDeadline, ok := calledCtx.Deadline()
	require.True(t, ok)
	require.Equal(t, wantDeadline, gotDeadline)
	require.ErrorIs(t, calledCtx.Err(), context.Canceled)
	require.NoError(t, ctx.Err(), "finishing the call must not cancel its parent")
}

// A cancellation during either authentication or the tool call must reach
// that operation without waiting for the independent call timeout.
func TestRunCommandCancellation(t *testing.T) {
	for _, stage := range []string{"open", "call"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &runner{
				flag:   &flag{},
				stdout: io.Discard,
				open: func(openCtx context.Context) (*client.Session, error) {
					if stage == "open" {
						cancel()
						if err := openCtx.Err(); err != nil {
							return nil, err
						}
					}
					return &client.Session{Caller: contextCaller(func(callCtx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
						cancel()
						return json.RawMessage(`{}`), callCtx.Err()
					})}, nil
				},
			}
			cmd := &cobra.Command{Use: name, RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
			r.flag.Init(cmd)
			cmd.SetArgs([]string{})

			require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
		})
	}
}
