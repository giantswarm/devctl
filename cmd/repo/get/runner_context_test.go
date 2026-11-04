package get

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type contextCaller func(context.Context, string, map[string]any) (json.RawMessage, error)

func (c contextCaller) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return c(ctx, tool, args)
}

func contextCommand(r *Runner) *cobra.Command {
	cmd := &cobra.Command{
		Use: "record", Args: cobra.ExactArgs(1), RunE: r.Run,
		SilenceErrors: true, SilenceUsage: true,
	}
	cmd.SetArgs([]string{"my-service"})
	return cmd
}

// A canceled command must reach neither a fresh manager request nor any
// output when opening the session honors cancellation.
func TestRunCommandCanceledBeforeOpen(t *testing.T) {
	for _, tool := range []string{manager.ToolGetRepository, manager.ToolRefreshRepository} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancel()
			var out bytes.Buffer
			calls := 0
			caller := contextCaller(func(context.Context, string, map[string]any) (json.RawMessage, error) {
				calls++
				return json.RawMessage(`{"repository":"giantswarm/my-service"}`), nil
			})
			r := &Runner{
				flag: &flag{Flags: client.Flags{Output: client.OutputText}}, stdout: &out, tool: tool,
				open: func(openCtx context.Context) (*client.Session, error) {
					if err := openCtx.Err(); err != nil {
						return nil, err
					}
					return &client.Session{Caller: caller}, nil
				},
			}
			require.ErrorIs(t, contextCommand(r).ExecuteContext(ctx), context.Canceled)
			require.Zero(t, calls)
			require.Empty(t, out.String())
		})
	}
}

// Authentication and the manager call inherit the command's values and
// shorter deadline. The derived call context is released on return.
func TestRunCommandDeadline(t *testing.T) {
	type contextKey struct{}
	for _, tool := range []string{manager.ToolGetRepository, manager.ToolRefreshRepository} {
		t.Run(tool, func(t *testing.T) {
			deadline := time.Now().Add(30 * time.Second)
			ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "caller"), deadline)
			defer cancel()
			var callCtx context.Context
			var out bytes.Buffer
			checkContext := func(ctx context.Context) {
				require.Equal(t, "caller", ctx.Value(contextKey{}))
				got, ok := ctx.Deadline()
				require.True(t, ok)
				require.Equal(t, deadline, got)
			}
			caller := contextCaller(func(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
				checkContext(ctx)
				callCtx = ctx
				return json.RawMessage(`{"repository":"giantswarm/my-service"}`), nil
			})
			r := &Runner{
				flag: &flag{Flags: client.Flags{Output: client.OutputText}}, stdout: &out, tool: tool,
				open: func(ctx context.Context) (*client.Session, error) {
					checkContext(ctx)
					return &client.Session{Caller: caller}, nil
				},
			}
			require.NoError(t, contextCommand(r).ExecuteContext(ctx))
			require.NotNil(t, callCtx)
			require.ErrorIs(t, callCtx.Err(), context.Canceled)
			require.NoError(t, ctx.Err())
		})
	}
}

// Cancellation during a call must be visible immediately to that call.
// The fake cancels synchronously; no timer, server or goroutine is needed.
func TestRunCommandCanceledDuringCall(t *testing.T) {
	for _, tool := range []string{manager.ToolGetRepository, manager.ToolRefreshRepository} {
		t.Run(tool, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out bytes.Buffer
			caller := contextCaller(func(callCtx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
				cancel()
				if err := callCtx.Err(); err != nil {
					return nil, err
				}
				return json.RawMessage(`{"repository":"giantswarm/my-service"}`), nil
			})
			r := &Runner{
				flag: &flag{Flags: client.Flags{Output: client.OutputText}}, stdout: &out, tool: tool,
				open: func(context.Context) (*client.Session, error) {
					return &client.Session{Caller: caller}, nil
				},
			}
			require.ErrorIs(t, contextCommand(r).ExecuteContext(ctx), context.Canceled)
			require.Empty(t, out.String())
		})
	}
}
