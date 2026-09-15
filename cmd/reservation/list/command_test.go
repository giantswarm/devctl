package list_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/list"
)

const repoDirFlag = "--repo-dir"

// newCommand builds the list command the way cmd/reservation does.
func newCommand(t *testing.T, stdout io.Writer) *cobra.Command {
	t.Helper()

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := list.New(list.Config{Logger: logger, Stderr: io.Discard, Stdout: stdout})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

// TestRefusesAMissingCluster checks that a missing --cluster is refused by the
// flags, before the command ever opens --repo-dir.
func TestRefusesAMissingCluster(t *testing.T) {
	cmd := newCommand(t, io.Discard)
	cmd.SetArgs([]string{repoDirFlag, t.TempDir()})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !list.IsInvalidFlag(err) {
		t.Fatalf("expected an invalid-flag error, got %v", err)
	}
}

// TestListPrintsTheReservationFields is the CLI-level proof for "both
// commands work from a laptop": it reads a plain checkout, no clone, no
// GitHub token, and prints the fields user story 26 asks for.
func TestListPrintsTheReservationFields(t *testing.T) {
	const cluster, chart = "graveler", "hello-world"
	dir := t.TempDir()
	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	if err := os.MkdirAll(clusterDir, 0o750); err != nil {
		t.Fatal(err)
	}
	configMap := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"data:\n" +
		"  " + chart + ": '{user: alice, branch: fix/crash, pr: giantswarm/hello-world#123, scope: app, from: 2026-09-15T10:00:00Z, until: 2026-09-15T20:00:00Z}'\n"
	if err := os.WriteFile(filepath.Join(clusterDir, "configmap-reservations.yaml"), []byte(configMap), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{chart, "alice", "fix/crash", "giantswarm/hello-world#123", "app", "2026-09-15"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

// TestListReportsNoneOnAClusterWithNoReservations checks the empty case reads
// as "nothing to see", not a blank table.
func TestListReportsNoneOnAClusterWithNoReservations(t *testing.T) {
	const cluster = "graveler"
	dir := t.TempDir()
	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	if err := os.MkdirAll(clusterDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clusterDir, "configmap-reservations.yaml"), []byte("data: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout.String(), "No active reservations") {
		t.Errorf("output does not say there is nothing to release: %s", stdout.String())
	}
}
