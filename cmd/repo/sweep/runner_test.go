package sweep

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

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

type failingWriter struct {
	remaining int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	n := min(w.remaining, len(p))
	w.remaining -= n
	if n < len(p) {
		return n, io.ErrClosedPipe
	}
	return n, nil
}

func TestRunOutputError(t *testing.T) {
	firstLine := "sweep started as alice (a member of team-bumblebee); `devctl repo list` shows the new records when it is done\n"
	for _, tc := range []struct {
		name      string
		output    string
		answer    string
		remaining int
	}{
		{name: "text", output: client.OutputText, answer: sweepAnswer},
		{name: "missing summary", output: client.OutputText, answer: sweepAnswer, remaining: len(firstLine)},
		{name: "last summary", output: client.OutputText, answer: `{"started":true,"login":"alice","teams":["team-bumblebee"],"last":{"finishedAt":"2026-09-22T03:00:00Z","duration":"41m2s","repositories":1790}}`, remaining: len(firstLine)},
		{name: "json", output: client.OutputJSON, answer: sweepAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := &runner{
				flag:   &flag{Flags: client.Flags{Output: tc.output}},
				stdout: &failingWriter{remaining: tc.remaining},
				open: func(context.Context) (*client.Session, error) {
					return &client.Session{Caller: callerFunc(func(context.Context, string, map[string]any) (json.RawMessage, error) {
						calls++
						return json.RawMessage(tc.answer), nil
					})}, nil
				},
			}
			require.ErrorIs(t, r.run(context.Background()), io.ErrClosedPipe)
			require.Equal(t, 1, calls, "an output failure must not repeat the sweep request")
		})
	}
}
