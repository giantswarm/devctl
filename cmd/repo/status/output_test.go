package status

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
)

type statusFailingWriter struct {
	remaining int
	err       error
}

func (w *statusFailingWriter) Write(p []byte) (int, error) {
	n := min(len(p), w.remaining)
	w.remaining -= n
	if n < len(p) {
		return n, w.err
	}
	return n, nil
}

// Failed and partial output writes must fail the command in either format.
func TestRunOutputError(t *testing.T) {
	writeErr := errors.New("output unavailable")
	for _, format := range []string{client.OutputText, client.OutputJSON} {
		for _, tc := range []struct {
			name      string
			remaining int
		}{
			{name: "first write"},
			{name: "partial write", remaining: 32},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				r, caller, _ := newRunner(record, format)
				w := &statusFailingWriter{remaining: tc.remaining, err: writeErr}
				r.stdout = w
				require.ErrorIs(t, r.run(context.Background(), "my-service"), writeErr)
				require.NotEmpty(t, caller.tool)
				require.Zero(t, w.remaining)
			})
		}
	}
}
