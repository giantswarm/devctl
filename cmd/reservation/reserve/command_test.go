package reserve_test

import (
	"io"
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

// TestExclusiveFlagDefaultsToFalse checks the cobra surface a caller like
// slice 09 depends on: an --exclusive bool flag, off by default, so a plain
// reserve keeps requesting the app scope it always has.
func TestExclusiveFlagDefaultsToFalse(t *testing.T) {
	cmd := newCommand(t)

	f := cmd.Flags().Lookup("exclusive")
	if f == nil {
		t.Fatal("no --exclusive flag registered")
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
