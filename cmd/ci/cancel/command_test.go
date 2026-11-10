package cancel

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidCancellationHasDocument(t *testing.T) {
	for _, args := range [][]string{{}, {"o/r"}, {"o/r", "w", "--stale"}, {"o/r", "--stale", "--pipeline"}, {"o/r", "w", "--dry-run=bad"}} {
		var out bytes.Buffer
		cmd, err := New(Config{Stdout: &out, Stderr: &out})
		require.NoError(t, err)
		cmd.SetArgs(args)
		require.Error(t, cmd.Execute())
		var doc struct {
			Command  string `json:"command"`
			ExitCode int    `json:"exitCode"`
		}
		require.NoError(t, json.NewDecoder(&out).Decode(&doc))
		require.Equal(t, "ci cancel", doc.Command)
		require.Equal(t, 7, doc.ExitCode)
	}
}
