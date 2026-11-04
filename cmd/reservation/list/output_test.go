package list_test

import (
	"errors"
	"io"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestListReportsOutputErrors(t *testing.T) {
	const cluster = "graveler"
	const populated = "data:\n" +
		"  hello-world: '{user: alice, branch: fix/crash, pr: \"\", scope: app, from: 2026-09-15T10:00:00Z, until: 2026-09-15T20:00:00Z}'\n"

	for _, tc := range []struct {
		name      string
		configMap string
		output    string
	}{
		{"empty table", "data: {}\n", "table"},
		{"populated table", populated, "table"},
		{"empty JSON", "data: {}\n", "json"},
		{"populated JSON", populated, "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newCheckout(t, map[string]string{
				"management-clusters/" + cluster + "/configmap-reservations.yaml": tc.configMap,
			})
			cmd := newCommand(t, failingWriter{})
			cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster, "--output", tc.output})

			if err := cmd.Execute(); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("got %v, want the output writer's error", err)
			}
		})
	}
}
