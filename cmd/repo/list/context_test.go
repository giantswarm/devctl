package list

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type contextCallerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f contextCallerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

// A canceled Cobra context must reach session opening, which can refresh
// authentication before the manager call starts.
func TestRunCanceledCommandContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	r := &runner{
		flag:   &flag{Flags: client.Flags{Output: client.OutputText}, Scope: "mine"},
		stdout: &out,
		open: func(ctx context.Context) (*client.Session, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, errors.New("session opening lost the canceled context")
		},
	}
	cmd := &cobra.Command{Use: "list", RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
	r.flag.Init(cmd)
	cmd.SetArgs([]string{})
	require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
	require.Empty(t, out.String())
}

// Cancellation during session opening, the caller's shorter deadline, and
// context values must survive the bounded manager call.
func TestRunCommandContextReachesCall(t *testing.T) {
	type contextKey struct{}
	deadline := time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "caller"), deadline)
	defer cancel()
	var opened, called context.Context
	var out bytes.Buffer
	r := &runner{
		flag:   &flag{Flags: client.Flags{Output: client.OutputText}, Scope: "mine"},
		stdout: &out,
		open: func(ctx context.Context) (*client.Session, error) {
			opened = ctx
			cancel()
			return &client.Session{Caller: contextCallerFunc(func(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
				called = ctx
				return nil, ctx.Err()
			})}, nil
		},
	}
	cmd := &cobra.Command{Use: "list", RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
	r.flag.Init(cmd)
	cmd.SetArgs([]string{})
	require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
	require.NotNil(t, opened)
	require.NotNil(t, called)
	require.Equal(t, "caller", opened.Value(contextKey{}))
	require.Equal(t, "caller", called.Value(contextKey{}))
	for _, got := range []context.Context{opened, called} {
		gotDeadline, ok := got.Deadline()
		require.True(t, ok)
		require.Equal(t, deadline, gotDeadline)
	}
	require.Empty(t, out.String())
}
