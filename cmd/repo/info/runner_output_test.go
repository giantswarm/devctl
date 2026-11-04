package info

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

type infoResultCaller struct {
	payload json.RawMessage
}

func (c infoResultCaller) Call(context.Context, string, map[string]any) (json.RawMessage, error) {
	return c.payload, nil
}

type outputWriterFunc func([]byte) (int, error)

func (f outputWriterFunc) Write(p []byte) (int, error) { return f(p) }

// A failed stdout must fail the command in either format. The large text
// case also exercises an error before the renderer's final buffered flush.
func TestRunOutputErrors(t *testing.T) {
	for _, format := range []string{client.OutputText, client.OutputJSON} {
		for _, size := range []string{"small", "large"} {
			t.Run(format+"/"+size, func(t *testing.T) {
				var info manager.Info
				info.Version = "0.25.2"
				if size == "large" {
					info.Inventory.Error = strings.Repeat("diagnostic ", 1024)
				}
				payload, err := json.Marshal(info)
				require.NoError(t, err)
				writeErr := errors.New("stdout failed")
				r := &runner{
					flag: &flag{Flags: client.Flags{Output: format}},
					stdout: outputWriterFunc(func(p []byte) (int, error) {
						return len(p) / 2, writeErr
					}),
					open: func(context.Context) (*client.Session, error) {
						return &client.Session{Caller: infoResultCaller{payload: payload}}, nil
					},
				}

				require.ErrorIs(t, r.run(context.Background()), writeErr)
			})
		}
	}
}
