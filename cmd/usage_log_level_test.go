package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func TestExecuteAgentInvalidLogLevel(t *testing.T) {
	for _, command := range [][]string{{"pr", testWaitCommandName}, {"release", testWaitCommandName}, {"auth", "status"}, {"ci", "jobs"}, {"rollout", testWaitCommandName}} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := newTestRoot(t, &stdout)
			root.SetArgs(append(command, "--log-level", "debgu"))
			if code := Execute(root, &stderr); code != agentcli.ExitUsage {
				t.Errorf("exit %d, want %d", code, agentcli.ExitUsage)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr %q, want none", stderr.String())
			}
			var doc agentcli.Envelope
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
			}
			if doc.Command != strings.Join(command, " ") || doc.ExitCode != agentcli.ExitUsage || doc.Verdict != agentcli.VerdictUsage || !strings.Contains(doc.Reason, "log-level") {
				t.Errorf("envelope: %+v", doc)
			}
		})
	}
}

func TestExecuteHumanInvalidLogLevel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newTestRoot(t, &stdout)
	root.SetArgs([]string{"version", "--log-level", "debgu"})
	if code := Execute(root, &stderr); code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want none", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--log-level") || !strings.Contains(stderr.String(), "Usage: devctl version") || !strings.Contains(stderr.String(), "Run 'devctl version --help'") {
		t.Errorf("stderr %q, want the flag error, usage and help pointer", stderr.String())
	}
}

// Keep the logging flag a string with its existing default and accepted
// logrus levels, including the warning alias and case-insensitive spelling.
func TestLogLevelFlagValues(t *testing.T) {
	for _, level := range []string{"panic", "fatal", "error", "warn", "warning", "info", "debug", "trace", "DEBUG"} {
		t.Run(level, func(t *testing.T) {
			c := &cobra.Command{Use: "test"}
			f := &flag{}
			f.Init(c)
			if got, err := c.PersistentFlags().GetString(flagLogLevel); err != nil || got != "info" {
				t.Fatalf("default level %q, %v; want info", got, err)
			}
			if err := c.ParseFlags([]string{"--log-level", level}); err != nil {
				t.Fatal(err)
			}
			if got, err := c.PersistentFlags().GetString(flagLogLevel); err != nil || got != level || f.LogLevel != level {
				t.Errorf("level %q, %v, stored %q; want %q", got, err, f.LogLevel, level)
			}
		})
	}
}
