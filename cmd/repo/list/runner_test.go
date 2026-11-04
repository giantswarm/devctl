package list

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type callerFunc func(context.Context, string, map[string]any) (json.RawMessage, error)

func (f callerFunc) Call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	return f(ctx, tool, args)
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Text output must report failures in the summary and buffered table, just
// as JSON output reports a writer failure.
func TestRunOutputErrors(t *testing.T) {
	const listing = `{"scope":"all","note":"inventory note","sweep":null,"total":1,"matched":1,"shown":1,"repositories":[{"repository":"giantswarm/service","team":"team-bumblebee","setup":{}}]}`
	const empty = `{"scope":"all","sweep":null,"total":0,"matched":0,"shown":0,"repositories":[]}`
	for _, tc := range []struct {
		name, output, payload, failOn string
	}{
		{"heading", client.OutputText, listing, "scope all"},
		{"note", client.OutputText, listing, "note: inventory note"},
		{"sweep summary", client.OutputText, listing, "last sweep: none yet"},
		{"no matches", client.OutputText, empty, "no repositories match"},
		{"table heading", client.OutputText, listing, "REPOSITORY"},
		{"repository row", client.OutputText, listing, "giantswarm/service"},
		{"JSON", client.OutputJSON, listing, `"scope"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outputError := errors.New("output unavailable")
			failed := false
			r := &runner{
				flag: &flag{Flags: client.Flags{Output: tc.output}, Scope: "all"},
				stdout: writerFunc(func(p []byte) (int, error) {
					if bytes.Contains(p, []byte(tc.failOn)) {
						failed = true
						return 0, outputError
					}
					return len(p), nil
				}),
				open: func(context.Context) (*client.Session, error) {
					return &client.Session{Caller: callerFunc(func(context.Context, string, map[string]any) (json.RawMessage, error) {
						return json.RawMessage(tc.payload), nil
					})}, nil
				},
			}
			require.ErrorIs(t, r.run(context.Background()), outputError)
			require.True(t, failed, "the selected output write must have been reached")
		})
	}
}
