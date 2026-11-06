package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestExecuteThen_RunsAfterTheExecutedCommandWhateverItsOutcome(t *testing.T) {
	for name, runErr := range map[string]error{"green": nil, "failed": errors.New("boom")} {
		t.Run(name, func(t *testing.T) {
			root := &cobra.Command{Use: "devctl", SilenceErrors: true, SilenceUsage: true}
			wait := &cobra.Command{Use: "wait", RunE: func(*cobra.Command, []string) error { return runErr }}
			root.AddCommand(wait)
			root.SetArgs([]string{"wait"})
			var after *cobra.Command

			code := ExecuteThen(root, &bytes.Buffer{}, func(executed *cobra.Command) { after = executed })

			require.Same(t, wait, after)
			require.Equal(t, runErr != nil, code != 0, "the exit code stays the command's")
		})
	}
}
