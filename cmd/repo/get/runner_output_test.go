package get

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type outputCaller struct {
	payload json.RawMessage
	tool    string
	args    map[string]any
}

func (c *outputCaller) Call(_ context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	c.tool, c.args = tool, args
	return c.payload, nil
}

type failingOutput struct {
	remaining int
	written   int
	err       error
}

func (w *failingOutput) Write(p []byte) (int, error) {
	n := min(len(p), w.remaining)
	w.remaining -= n
	w.written += n
	if n < len(p) {
		return n, w.err
	}
	return n, nil
}

// Both verbs must report a lost record, including a failure while the
// renderer fills the buffer and a failure flushing its final bytes.
func TestRunOutputErrors(t *testing.T) {
	writeErr := errors.New("output full")
	for _, tool := range []string{manager.ToolGetRepository, manager.ToolRefreshRepository} {
		for _, format := range []string{client.OutputText, client.OutputJSON} {
			for _, tc := range []struct {
				name      string
				entry     string
				failAfter int
			}{
				{name: "first write"},
				{name: "partial final flush", failAfter: 8},
				{name: "during rendering", entry: "- name: my-service\n  description: |\n" + strings.Repeat("    details\n", 512), failAfter: 64},
			} {
				t.Run(tool+"/"+format+"/"+tc.name, func(t *testing.T) {
					payload, err := json.Marshal(&manager.Record{
						Repository: "giantswarm/my-service",
						Declaration: &manager.Declaration{
							Team: "team-example", Accepted: true, Entry: tc.entry,
						},
					})
					require.NoError(t, err)
					caller := &outputCaller{payload: payload}
					out := &failingOutput{remaining: tc.failAfter, err: writeErr}
					r := &Runner{
						flag:   &flag{Flags: client.Flags{Output: format}},
						stdout: out,
						tool:   tool,
						open: func(context.Context) (*client.Session, error) {
							return &client.Session{Caller: caller}, nil
						},
					}

					require.ErrorIs(t, r.run(context.Background(), "my-service"), writeErr)
					require.Equal(t, tc.failAfter, out.written)
					require.Equal(t, tool, caller.tool)
					require.Equal(t, map[string]any{"repository": "my-service"}, caller.args)
				})
			}
		}
	}
}

// Buffering the text must still deliver the complete record, and JSON
// must retain fields outside devctl's text model.
func TestRunOutputRecord(t *testing.T) {
	const payload = `{"repository":"giantswarm/my-service","extra":{"kept":true}}`
	for _, tool := range []string{manager.ToolGetRepository, manager.ToolRefreshRepository} {
		for _, format := range []string{client.OutputText, client.OutputJSON} {
			t.Run(tool+"/"+format, func(t *testing.T) {
				var out bytes.Buffer
				caller := &outputCaller{payload: json.RawMessage(payload)}
				r := &Runner{
					flag:   &flag{Flags: client.Flags{Output: format}},
					stdout: &out,
					tool:   tool,
					open: func(context.Context) (*client.Session, error) {
						return &client.Session{Caller: caller}, nil
					},
				}
				require.NoError(t, r.run(context.Background(), " giantswarm/my-service "))
				require.Equal(t, tool, caller.tool)
				require.Equal(t, map[string]any{"repository": "giantswarm/my-service"}, caller.args)
				if format == client.OutputJSON {
					require.JSONEq(t, payload, out.String())
				} else {
					require.Equal(t, "giantswarm/my-service\n"+
						"declaration: none -- no team file declares this repository (the Unassigned scope); `devctl repo adopt` declares it\n"+
						"github: gone -- the repository does not exist on GitHub\n"+
						"set-up: not checked\n", out.String())
				}
			})
		}
	}
}
