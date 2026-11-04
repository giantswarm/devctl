package status

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type statusCallerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f statusCallerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

// Both session opening and the manager call inherit cancellation from Cobra.
// The fakes inspect the context synchronously, without waiting on a server.
func TestRunContextCancellation(t *testing.T) {
	for _, format := range []string{client.OutputText, client.OutputJSON} {
		t.Run(format+"/session", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r, caller, out := newRunner(record, format)
			r.open = func(openCtx context.Context) (*client.Session, error) {
				require.ErrorIs(t, openCtx.Err(), context.Canceled)
				return nil, context.Canceled
			}
			cmd := &cobra.Command{RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
			cmd.SetArgs([]string{"my-service"})
			require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
			require.Empty(t, caller.tool)
			require.Empty(t, out.String())
		})
		t.Run(format+"/manager", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r, _, out := newRunner(record, format)
			called := false
			r.open = func(context.Context) (*client.Session, error) {
				cancel()
				return &client.Session{Caller: statusCallerFunc(func(callCtx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
					called = true
					require.ErrorIs(t, callCtx.Err(), context.Canceled)
					require.Equal(t, manager.ToolGetRepository, tool)
					require.Equal(t, "my-service", args["repository"])
					return nil, context.Canceled
				})}, nil
			}
			cmd := &cobra.Command{RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
			cmd.SetArgs([]string{"my-service"})
			require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
			require.True(t, called)
			require.Empty(t, out.String())
		})
	}
}

type statusContextKey struct{}

// A caller's earlier deadline and values reach authentication and the tool.
// Finishing the call cancels its child context without canceling the caller.
func TestRunContextDeadline(t *testing.T) {
	deadline := time.Now().Add(callTimeout / 2)
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), statusContextKey{}, "request"), deadline)
	defer cancel()
	r, _, out := newRunner(record, client.OutputText)
	var callContext context.Context
	check := func(ctx context.Context) {
		t.Helper()
		got, ok := ctx.Deadline()
		require.True(t, ok)
		require.Equal(t, deadline, got)
		require.Equal(t, "request", ctx.Value(statusContextKey{}))
		require.NoError(t, ctx.Err())
	}
	r.open = func(openCtx context.Context) (*client.Session, error) {
		check(openCtx)
		return &client.Session{Caller: statusCallerFunc(func(callCtx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
			check(callCtx)
			callContext = callCtx
			return json.RawMessage(record), nil
		})}, nil
	}
	cmd := &cobra.Command{RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
	cmd.SetArgs([]string{"my-service"})
	require.NoError(t, cmd.ExecuteContext(ctx))
	require.Contains(t, out.String(), "converged: set up as declared")
	require.NotNil(t, callContext)
	require.ErrorIs(t, callContext.Err(), context.Canceled)
	require.NoError(t, ctx.Err())
}
