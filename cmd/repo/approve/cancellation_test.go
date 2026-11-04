package approve

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type cancellationCaller func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f cancellationCaller) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

// A cancelled invocation must not read credentials or send an approval.
func TestRunCancelledBeforeSession(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "expired deadline"}[deadline], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			want := context.Canceled
			if deadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				want = context.DeadlineExceeded
			}
			opened, calls := 0, 0
			var out bytes.Buffer
			r := &runner{flag: &flag{Flags: client.Flags{Output: client.OutputText}}, stdout: &out,
				open: func(context.Context) (*client.Session, error) {
					opened++
					return &client.Session{Caller: cancellationCaller(func(context.Context, string, map[string]any) (json.RawMessage, error) {
						calls++
						return json.RawMessage(`{"pullRequest":6179,"member":true,"merged":true}`), nil
					})}, nil
				},
			}
			cmd := &cobra.Command{}
			cmd.SetContext(ctx)
			err := r.Run(cmd, []string{"6179"})
			require.ErrorIs(t, err, want)
			require.Zero(t, opened)
			require.Zero(t, calls)
			require.Empty(t, out.String())
		})
	}
}

// Both session opening and the approval inherit the caller's context.
func TestRunCancellationDuringApproval(t *testing.T) {
	type contextKey struct{}
	for _, stage := range []string{"session", "approval"} {
		t.Run(stage, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "caller"))
			defer cancel()
			var openedCtx, calledCtx context.Context
			calls := 0
			var out bytes.Buffer
			r := &runner{flag: &flag{Flags: client.Flags{Output: client.OutputText}}, stdout: &out,
				open: func(ctx context.Context) (*client.Session, error) {
					openedCtx = ctx
					if stage == "session" {
						cancel()
						if err := ctx.Err(); err != nil {
							return nil, err
						}
					}
					return &client.Session{Caller: cancellationCaller(func(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
						calls++
						calledCtx = ctx
						cancel()
						if err := ctx.Err(); err != nil {
							return nil, err
						}
						return json.RawMessage(`{"pullRequest":6179,"member":true,"merged":true}`), nil
					})}, nil
				},
			}
			cmd := &cobra.Command{}
			cmd.SetContext(parent)
			err := r.Run(cmd, []string{"6179"})
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, "caller", openedCtx.Value(contextKey{}))
			if stage == "session" {
				require.Zero(t, calls)
			} else {
				require.Equal(t, "caller", calledCtx.Value(contextKey{}))
				require.Equal(t, 1, calls)
			}
			require.Empty(t, out.String())
		})
	}
}

// The earlier parent deadline wins over the two-minute call bound, and the
// derived call context is released once its synchronous call has returned.
func TestRunApprovalDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	parent, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var calledCtx context.Context
	var out bytes.Buffer
	r := &runner{flag: &flag{Flags: client.Flags{Output: client.OutputText}}, stdout: &out,
		open: func(context.Context) (*client.Session, error) {
			return &client.Session{Caller: cancellationCaller(func(ctx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
				calledCtx = ctx
				return json.RawMessage(`{"pullRequest":6179,"member":true,"merged":true}`), nil
			})}, nil
		},
	}
	cmd := &cobra.Command{}
	cmd.SetContext(parent)
	require.NoError(t, r.Run(cmd, []string{"6179"}))
	got, ok := calledCtx.Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, got)
	require.ErrorIs(t, calledCtx.Err(), context.Canceled)
	require.NoError(t, parent.Err())
	require.Contains(t, out.String(), "merged\n")
}
