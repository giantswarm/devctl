package cmd

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/env"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/project"
)

// newTestRoot is the command tree devctl runs, writing into stdout, with the
// version check that precedes every run turned off.
func newTestRoot(t *testing.T, stdout *bytes.Buffer) *cobra.Command {
	t.Helper()
	t.Setenv(env.DevctlUnsafeForceVersion.Key(), project.Version())
	logger, err := micrologger.New(micrologger.Config{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := New(Config{Logger: logger, Stdout: stdout, Stderr: stdout})
	if err != nil {
		t.Fatal(err)
	}
	root.SetOut(stdout)
	root.SetErr(stdout)
	return root
}

// TestExecuteWrongCalls runs wrong calls through the command tree: each is
// named on stderr with the usage line and the pointer to the help, exit 2,
// and none reaches a runner, a token or the network.
func TestExecuteWrongCalls(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		stderr string
	}{
		{
			name: "missing argument",
			args: []string{"repo", "status"},
			stderr: "Error: Missing [OWNER/]REPOSITORY\n" +
				"Usage: devctl repo status [flags] [OWNER/]REPOSITORY\n" +
				"Run 'devctl repo status --help' for more information.\n",
		},
		{
			name: "missing second argument",
			args: []string{"repo", "set-lifecycle", "my-service"},
			stderr: "Error: Missing deprecated|archived|deleted\n" +
				"Usage: devctl repo set-lifecycle [flags] [OWNER/]REPOSITORY deprecated|archived|deleted\n" +
				"Run 'devctl repo set-lifecycle --help' for more information.\n",
		},
		{
			name: "unexpected argument",
			args: []string{"repo", "status", "a", "b"},
			stderr: "Error: Unexpected argument \"b\"\n" +
				"Usage: devctl repo status [flags] [OWNER/]REPOSITORY\n" +
				"Run 'devctl repo status --help' for more information.\n",
		},
		{
			name: "argument to a command that takes none",
			args: []string{"gen", "circleci", "x", "y"},
			stderr: "Error: Unexpected arguments \"x\" \"y\"\n" +
				"Usage: devctl gen circleci [flags]\n" +
				"Run 'devctl gen circleci --help' for more information.\n",
		},
		{
			name: "invalid argument",
			args: []string{"completion", "tcsh"},
			stderr: "Error: Invalid argument \"tcsh\" for \"devctl completion\"\n" +
				"Usage: devctl completion [bash|zsh|fish|powershell]\n" +
				"Run 'devctl completion --help' for more information.\n",
		},
		{
			name: "unknown subcommand",
			args: []string{"repo", "statsu"},
			stderr: "Error: Unknown command \"statsu\" for \"devctl repo\"; did you mean \"status\"?\n" +
				"Run 'devctl repo --help' for more information.\n",
		},
		{
			name: "unknown command",
			args: []string{"reop"},
			stderr: "Error: Unknown command \"reop\" for \"devctl\"; did you mean \"repo\"?\n" +
				"Run 'devctl --help' for more information.\n",
		},
		{
			name: "unknown flag",
			args: []string{"gen", "circleci", "--bogus"},
			stderr: "Error: Unknown flag: --bogus\n" +
				"Usage: devctl gen circleci [flags]\n" +
				"Run 'devctl gen circleci --help' for more information.\n",
		},
		{
			name: "a runner's flag validation",
			args: []string{"repo", "status", "--output", "xml", "my-service"},
			stderr: "Error: Invalid flag: --output must be text or json\n" +
				"Usage: devctl repo status [flags] [OWNER/]REPOSITORY\n" +
				"Run 'devctl repo status --help' for more information.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := newTestRoot(t, &stdout)
			root.SetArgs(tc.args)
			if code := Execute(root, &stderr); code != exitError {
				t.Errorf("exit %d, want %d", code, exitError)
			}
			if stderr.String() != tc.stderr {
				t.Errorf("stderr:\n%s\nwant:\n%s", stderr.String(), tc.stderr)
			}
		})
	}
}

func TestExecuteDebugPrintsTheStackTrace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newTestRoot(t, &stdout)
	root.SetArgs([]string{"repo", "status", "--output", "xml", "my-service", "--log-level", "debug"})
	Execute(root, &stderr)
	if !strings.Contains(stderr.String(), "runner.go:") {
		t.Errorf("no stack trace at --log-level debug:\n%s", stderr.String())
	}
}

// TestExecuteGroupWithoutArguments keeps a group's help, exit 0.
func TestExecuteGroupWithoutArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := newTestRoot(t, &stdout)
	root.SetArgs([]string{"repo"})
	if code := Execute(root, &stderr); code != 0 || stderr.Len() != 0 {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Available Commands:") {
		t.Errorf("no help on stdout:\n%s", stdout.String())
	}
}

// TestExecuteAgentCommandWrongCalls keeps the agent contract: a wrong call is
// the command's document on stdout with exit 7, nothing on stderr.
func TestExecuteAgentCommandWrongCalls(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "wait", "--bogus"},
		{"pr", "merge", "giantswarm/devctl", "1", "--timeout", "30"},
		{"release", "wait"},
		{"release", "wait", "giantswarm/devctl", "--pr", "x"},
		{"auth", "status", "extra"},
		{"auth", "login", "--bogus"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := newTestRoot(t, &stdout)
			root.SetArgs(args)
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
			if doc.Command != strings.Join(args[:2], " ") || doc.ExitCode != agentcli.ExitUsage || doc.Verdict != agentcli.VerdictUsage || doc.Reason == "" {
				t.Errorf("envelope: %+v", doc)
			}
		})
	}
}

// TestEveryCommandDeclaresItsArguments: a command without Args takes any
// argument silently; a group rejects what is none of its subcommands.
func TestEveryCommandDeclaresItsArguments(t *testing.T) {
	var stdout bytes.Buffer
	for _, c := range getAllCommands(newTestRoot(t, &stdout)) {
		if c.Args == nil {
			t.Errorf("%s declares no Args", c.CommandPath())
		}
	}
}

func TestParameters(t *testing.T) {
	cases := map[string][]string{
		"status":                             nil,
		"status [flags] [OWNER/]REPOSITORY":  {"[OWNER/]REPOSITORY"},
		"wait <owner/repo> <number>":         {"<owner/repo>", "<number>"},
		"setup [--remove] REPOSITORY":        {"REPOSITORY"},
		"replace [flags] PATTERN [GLOB ...]": {"PATTERN", "[GLOB ...]"},
	}
	for use, want := range cases {
		if got := parameters(&cobra.Command{Use: use}); !slices.Equal(got, want) {
			t.Errorf("parameters(%q) = %q, want %q", use, got, want)
		}
	}
}
