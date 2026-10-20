package reserve_test

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/reserve"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
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
	if !reservation.IsInvalidDuration(err) {
		t.Fatalf("expected an invalid-duration error, got %v", err)
	}
	for _, want := range []string{"30m", "4h", "2d"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not list %q: %s", want, err.Error())
		}
	}
}

// TestWarnsWithoutAPullRequest checks that a run with no --pull-request says
// so before it touches the GitOps checkout, and a run with one stays quiet.
func TestWarnsWithoutAPullRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"without", nil, true},
		{"with", []string{"--pull-request", "giantswarm/hello-world#1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := reserve.New(reserve.Config{Logger: logger, Stderr: &stderr, Stdout: io.Discard})
			if err != nil {
				t.Fatal(err)
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			// The checkout does not exist, so the run stops right after the warning.
			cmd.SetArgs(append([]string{
				"--gitops-dir", filepath.Join(t.TempDir(), "missing"),
				"--cluster", "graveler",
				"--app", "hello-world",
				"--branch", "fix/crash",
				"--user", "alice",
			}, tc.args...))
			_ = cmd.Execute()

			if got := strings.Contains(stderr.String(), "Warning: no --pull-request given"); got != tc.want {
				t.Errorf("warning shown = %v, want %v; stderr: %s", got, tc.want, stderr.String())
			}
		})
	}
}

// TestRefusesAMalformedPullRequest checks a typo or a URL is refused when it is
// written, not later by every reap run that reads it.
func TestRefusesAMalformedPullRequest(t *testing.T) {
	for _, pr := range []string{"123", "https://github.com/giantswarm/hello-world/pull/123", "giantswarm/hello-world#x"} {
		cmd := newCommand(t)
		cmd.SetArgs([]string{
			"--cluster", "graveler",
			"--app", "hello-world",
			"--branch", "fix/crash",
			"--user", "alice",
			"--pull-request", pr,
		})

		err := cmd.Execute()
		if err == nil || !reservation.IsInvalidConfig(err) {
			t.Errorf("--pull-request %q: expected an invalid-config refusal, got %v", pr, err)
		}
	}
}
