package merge

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

func TestStatusFlagErrorsUseStatusDocument(t *testing.T) {
	for _, arg := range []string{"--unknown", "--help=invalid"} {
		t.Run(arg, func(t *testing.T) {
			var stdout bytes.Buffer
			cmd, err := New(Config{Stdout: &stdout, Stderr: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs([]string{"status", arg})
			err = cmd.Execute()
			if agentcli.Exit(err) != agentcli.ExitUsage {
				t.Errorf("exit=%d, want usage: %v", agentcli.Exit(err), err)
			}
			doc := decode(t, &stdout)
			if doc["command"] != mergejob.StatusCommand {
				t.Errorf("command=%v, want %s", doc["command"], mergejob.StatusCommand)
			}
			if value, ok := doc["merge"]; !ok || value != nil {
				t.Errorf("missing status merge:null: %v", doc)
			}
			if !strings.Contains(doc["reason"].(string), "devctl pr merge status --help") {
				t.Errorf("reason names another command's help: %v", doc["reason"])
			}
		})
	}
}
