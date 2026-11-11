package reserve_test

import (
	"io"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/reserve"
)

// newCommand builds the reserve command the way cmd/reservation does.
func newCommand(t *testing.T) *cobra.Command {
	t.Helper()

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := reserve.New(reserve.Config{Logger: logger, Stderr: io.Discard, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

// TestExclusiveFlagDefaultsToFalse checks the cobra surface callers
// depend on: an --exclusive bool flag, off by default, so a plain
// reserve keeps requesting the app scope it always has.
func TestExclusiveFlagDefaultsToFalse(t *testing.T) {
	cmd := newCommand(t)

	f := cmd.Flags().Lookup("exclusive")
	if f == nil {
		t.Fatal("no --exclusive flag registered")
		return
	}
	if f.Value.Type() != "bool" {
		t.Errorf("--exclusive type: got %q, want bool", f.Value.Type())
	}
	if f.DefValue != "false" {
		t.Errorf("--exclusive default: got %q, want false", f.DefValue)
	}
}

// TestRefusesAWrongDurationBeforeItClones checks the refusal lands on the flags,
// not after a clone: a run that has already reached the network is slow, and it
// reports the wrong problem.
func TestRefusesAWrongDurationBeforeItClones(t *testing.T) {
	cmd := newCommand(t)
	cmd.SetArgs([]string{
		"--gitops-repo", "giantswarm/giantswarm-management-clusters",
		"--cluster", "graveler",
		"--app", "hello-world",
		"--branch", "fix/crash",
		"--user", "alice",
		"--duration", "4 hours",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reserve.IsInvalidFlag(err) {
		t.Fatalf("expected an invalid-flag error, got %v", err)
	}
	for _, want := range []string{"30m", "4h", "2d"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not list %q: %s", want, err.Error())
		}
	}
}

// TestRefusesAMalformedPullRequest checks a typo or a URL is refused when it is
// written, not later by every reap run that reads it.
func TestRefusesAMalformedPullRequest(t *testing.T) {
	for _, pr := range []string{"123", "https://github.com/giantswarm/hello-world/pull/123", "giantswarm/hello-world#x", "a/b/c#1", "giantswarm/hello-world#0"} {
		cmd := newCommand(t)
		cmd.SetArgs([]string{
			"--cluster", "graveler",
			"--app", "hello-world",
			"--branch", "fix/crash",
			"--user", "alice",
			"--pull-request", pr,
		})

		err := cmd.Execute()
		if !reserve.IsInvalidFlag(err) {
			t.Errorf("--pull-request %q: expected an invalid-flag refusal, got %v", pr, err)
		}
	}
}
