package write

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type callerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f callerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// A successful manager call is not a successful command if the plan or
// outcome could not be written. Reporting that failure must not retry a write.
func TestRunTextOutputErrors(t *testing.T) {
	for _, mode := range []struct {
		name    string
		dryRun  bool
		payload string
	}{
		{name: "plan", dryRun: true, payload: `{"repository":"scratch","team":"team-example","accepted":true,"pullRequest":{"title":"archive scratch"}}`},
		{name: "committed", payload: `{"pullRequest":{"url":"https://example.invalid/pr/1","title":"archive scratch"}}`},
	} {
		t.Run(mode.name, func(t *testing.T) {
			for _, failure := range []struct {
				name   string
				writer writerFunc
				want   error
			}{
				{name: "failed", writer: func([]byte) (int, error) { return 0, io.ErrClosedPipe }, want: io.ErrClosedPipe},
				{name: "partial", writer: func(p []byte) (int, error) { return len(p) / 2, io.ErrClosedPipe }, want: io.ErrClosedPipe},
				{name: "short", writer: func(p []byte) (int, error) { return len(p) / 2, nil }, want: io.ErrShortWrite},
			} {
				t.Run(failure.name, func(t *testing.T) {
					calls := 0
					session := &client.Session{Caller: callerFunc(func(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
						calls++
						require.Equal(t, manager.ToolSetLifecycle, tool)
						require.Equal(t, "scratch", args["repository"])
						require.Equal(t, "archived", args["lifecycle"])
						return json.RawMessage(mode.payload), nil
					})}
					flags := &Flags{Flags: client.Flags{Output: client.OutputText}, DryRun: mode.dryRun}
					err := Run(context.Background(), session, failure.writer, flags, manager.ToolSetLifecycle, map[string]any{"repository": "scratch", "lifecycle": "archived"})
					require.ErrorIs(t, err, failure.want)
					require.Equal(t, 1, calls)
				})
			}
		})
	}
}

func TestRunTextOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		payload string
		want    string
	}{
		{
			name: "plan", dryRun: true,
			payload: `{"repository":"scratch","team":"team-example","accepted":true,"pullRequest":{"title":"archive scratch","branch":"archive-scratch","files":["team-example.yaml"],"as":"tester"}}`,
			want:    "dry run: nothing written\nscratch in team-example: accepted\npull request: archive scratch (branch archive-scratch, team-example.yaml) as tester\n",
		},
		{
			name:    "committed",
			payload: `{"pullRequest":{"url":"https://example.invalid/pr/1","title":"archive scratch","autoMerge":true}}`,
			want:    "pull request: https://example.invalid/pr/1 (archive scratch; opened, auto-merge armed)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := &client.Session{Caller: callerFunc(func(context.Context, string, map[string]any) (json.RawMessage, error) {
				return json.RawMessage(tc.payload), nil
			})}
			flags := &Flags{Flags: client.Flags{Output: client.OutputText}, DryRun: tc.dryRun}
			var out bytes.Buffer
			require.NoError(t, Run(context.Background(), session, &out, flags, manager.ToolSetLifecycle, map[string]any{"repository": "scratch", "lifecycle": "archived"}))
			require.Equal(t, tc.want, out.String())
		})
	}
}
