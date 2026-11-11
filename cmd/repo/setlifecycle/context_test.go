package setlifecycle

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
)

type contextCaller func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f contextCaller) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

func TestRunCanceledCommandContextAtOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	r := &runner{
		flag: &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}},
		open: func(ctx context.Context) (*client.Session, error) {
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			return nil, ctx.Err()
		},
	}

	require.ErrorIs(t, r.Run(cmd, []string{"scratch", "archived"}), context.Canceled)
}

// The command's deadline and request values reach both authentication and
// the manager. Cancellation during authentication must cancel the write too.
func TestRunCommandContextReachesWrite(t *testing.T) {
	type requestKey struct{}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), requestKey{}, "request"), time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	called := false
	r := &runner{
		flag:   &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}},
		stdout: io.Discard,
		open: func(openCtx context.Context) (*client.Session, error) {
			require.Equal(t, "request", openCtx.Value(requestKey{}))
			openDeadline, ok := openCtx.Deadline()
			require.True(t, ok)
			require.Equal(t, deadline, openDeadline)
			cancel()
			return &client.Session{Caller: contextCaller(func(callCtx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
				called = true
				require.Equal(t, "request", callCtx.Value(requestKey{}))
				callDeadline, ok := callCtx.Deadline()
				require.True(t, ok)
				require.Equal(t, deadline, callDeadline)
				return nil, callCtx.Err()
			})}, nil
		},
	}

	require.ErrorIs(t, r.Run(cmd, []string{"scratch", "archived"}), context.Canceled)
	require.True(t, called)
}
