package sweep

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// Started, already running and the last summary each have their sentence.
func TestPrint(t *testing.T) {
	at := time.Date(2026, 9, 22, 3, 0, 0, 0, time.UTC)
	last := &manager.SweepSummary{FinishedAt: at, Duration: "41m2s", Repositories: 1790, Declared: 460, Undeclared: 1300, Gone: 3, Archived: 27, EngineChecks: 460, Errors: []string{"x"}}
	var out bytes.Buffer
	print(&out, &manager.Sweep{Started: true, Running: true, Login: "alice", Teams: []string{"team-bumblebee", "team-planeteers"}, Last: last})
	require.Contains(t, out.String(), "sweep started as alice (a member of team-bumblebee, team-planeteers)")
	require.Contains(t, out.String(), "last sweep: finished 2026-09-22T03:00:00Z in 41m2s: 1790 repositories (460 declared, 1300 undeclared, 3 gone, 27 archived), 460 engine checks, 1 errors")

	out.Reset()
	print(&out, &manager.Sweep{Running: true, Login: "alice"})
	require.Contains(t, out.String(), "a sweep is already running; none started (asked as alice)")
	require.Contains(t, out.String(), "last sweep: none yet")
}

type callerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f callerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

const sweepAnswer = `{"started":true,"running":true,"login":"alice","teams":["team-bumblebee"]}`

func TestRunCommandCancellation(t *testing.T) {
	for _, stage := range []string{"opening", "calling"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "opening" {
				cancel()
			}

			calls := 0
			r := &runner{
				flag:   &flag{Flags: client.Flags{Output: client.OutputText}},
				stdout: io.Discard,
				open: func(openCtx context.Context) (*client.Session, error) {
					if err := openCtx.Err(); err != nil {
						return nil, err
					}
					return &client.Session{Caller: callerFunc(func(callCtx context.Context, _ string, _ map[string]any) (json.RawMessage, error) {
						calls++
						cancel()
						return json.RawMessage(sweepAnswer), callCtx.Err()
					})}, nil
				},
			}
			cmd := &cobra.Command{Use: name, RunE: r.Run, SilenceErrors: true, SilenceUsage: true}
			cmd.SetArgs([]string{})
			require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
			if stage == "opening" {
				require.Zero(t, calls, "a canceled open must not start a sweep")
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestRunCommandDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	var callContext context.Context
	r := &runner{
		flag:   &flag{Flags: client.Flags{Output: client.OutputText}},
		stdout: io.Discard,
		open: func(openCtx context.Context) (*client.Session, error) {
			deadline, ok := openCtx.Deadline()
			require.True(t, ok, "authentication must inherit the command deadline")
			require.Equal(t, wantDeadline, deadline)
			return &client.Session{Caller: callerFunc(func(callCtx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
				callContext = callCtx
				deadline, ok := callCtx.Deadline()
				require.True(t, ok)
				require.Equal(t, wantDeadline, deadline, "the 60-second call timeout must not extend the caller's deadline")
				require.Equal(t, manager.ToolSweepInventory, tool)
				require.Nil(t, args)
				return json.RawMessage(sweepAnswer), nil
			})}, nil
		},
	}
	cmd := &cobra.Command{Use: name, RunE: r.Run}
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.ExecuteContext(ctx))
	require.NotNil(t, callContext)
	require.ErrorIs(t, callContext.Err(), context.Canceled, "the call context must be released on return")
	require.NoError(t, ctx.Err(), "returning must not cancel the caller's context")
}
