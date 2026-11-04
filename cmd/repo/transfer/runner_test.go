package transfer

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
	"github.com/giantswarm/devctl/v8/cmd/repo/internal/write"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type callerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f callerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

// Run must preserve the caller's cancellation and deadline while opening the
// session, including a cancellation that predates the command.
func TestRunUsesCommandContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Minute))
			},
			want: context.DeadlineExceeded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := tc.ctx()
			defer cancel()
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			var out bytes.Buffer
			opened := false
			r := &runner{
				flag:   &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}, ToTeam: "team-planeteers"},
				stdout: &out,
				open: func(got context.Context) (*client.Session, error) {
					opened = true
					if got.Err() == nil {
						return nil, errors.New("the command context was discarded")
					}
					return nil, got.Err()
				},
			}

			err := r.Run(cmd, []string{"old-tool"})
			require.ErrorIs(t, err, tc.want)
			require.True(t, opened)
			require.Empty(t, out.String())
		})
	}
}

// A cancellation during the manager call must reach the write's context,
// and a shorter command deadline must bound the shared two-minute timeout.
func TestRunCancellationReachesManager(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	var out bytes.Buffer
	calls := 0
	r := &runner{
		flag:   &flag{Flags: write.Flags{Flags: client.Flags{Output: client.OutputText}}, ToTeam: "team-planeteers"},
		stdout: &out,
		open: func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: callerFunc(func(callCtx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
				calls++
				require.Equal(t, manager.ToolTransferRepository, tool)
				require.Equal(t, map[string]any{"repository": "old-tool", "toTeam": "team-planeteers", "mode": "commit"}, args)
				got, ok := callCtx.Deadline()
				require.True(t, ok)
				require.Equal(t, deadline, got)
				cancel()
				return nil, callCtx.Err()
			})}, nil
		},
	}

	err := r.Run(cmd, []string{"old-tool"})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
	require.Empty(t, out.String())
}
