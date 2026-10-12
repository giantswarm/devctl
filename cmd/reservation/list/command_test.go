package list_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/list"
	"github.com/giantswarm/devctl/v8/internal/gittest"
)

const repoDirFlag = "--gitops-dir"

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

// newCheckout writes files into a git checkout whose origin is a local bare
// repo and pushes them, so the sync before the command finds a clean main equal
// to origin/main.
func newCheckout(t *testing.T, files map[string]string) string {
	t.Helper()

	origin := t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", "main")
	dir := t.TempDir()
	gittest.RunGit(t, dir, "init", "-b", "main")
	gittest.RunGit(t, dir, "remote", "add", "origin", origin)
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gittest.RunGit(t, dir, "add", "-A")
	gittest.RunGit(t, dir, "commit", "-m", "init")
	gittest.RunGit(t, dir, "push", "-u", "origin", "main")

	return dir
}

// TestRefusesAMissingCluster checks that a missing --cluster is refused by the
// flags, before the command ever opens --gitops-dir.
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
	until := time.Now().Add(10 * time.Hour).UTC()
	configMap := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"data:\n" +
		"  " + chart + ": '{user: alice, branch: fix/crash, pr: giantswarm/hello-world#123, scope: app, from: 2026-09-15T10:00:00Z, until: " + until.Format(time.RFC3339) + "}'\n"
	dir := newCheckout(t, map[string]string{"management-clusters/" + cluster + "/configmap-reservations.yaml": configMap})

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{chart, "alice", "fix/crash", "giantswarm/hello-world#123", "app", until.Format("2006-01-02")} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

// TestListMarksAnExpiredReservation checks that an entry whose Until has
// already passed is still shown, marked expired, so nobody mistakes the missing
// row for a free app while the reaper has not swept it yet.
func TestListMarksAnExpiredReservation(t *testing.T) {
	const cluster, expiredApp, activeApp = "graveler", "hello-world", "other-app"
	configMap := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"data:\n" +
		"  " + expiredApp + ": '{user: alice, branch: fix/crash, pr: \"\", scope: app, from: 2026-09-15T10:00:00Z, until: " +
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + "}'\n" +
		"  " + activeApp + ": '{user: bob, branch: feat/other, pr: \"\", scope: app, from: 2026-09-15T10:00:00Z, until: " +
		time.Now().Add(10*time.Hour).UTC().Format(time.RFC3339) + "}'\n"
	dir := newCheckout(t, map[string]string{"management-clusters/" + cluster + "/configmap-reservations.yaml": configMap})

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		switch {
		case strings.HasPrefix(line, expiredApp):
			if !strings.HasSuffix(line, "expired") {
				t.Errorf("expired row is not marked expired: %q", line)
			}
		case strings.HasPrefix(line, activeApp):
			if !strings.HasSuffix(line, "active") {
				t.Errorf("active row is not marked active: %q", line)
			}
		}
	}
	if !strings.Contains(stdout.String(), expiredApp) {
		t.Errorf("output does not show the expired app %q:\n%s", expiredApp, stdout.String())
	}
}

// TestListReportsNoneOnAClusterWithNoReservations checks the empty case reads
// as "nothing to see", not a blank table.
func TestListReportsNoneOnAClusterWithNoReservations(t *testing.T) {
	const cluster = "graveler"
	dir := newCheckout(t, map[string]string{"management-clusters/" + cluster + "/configmap-reservations.yaml": "data: {}\n"})

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{repoDirFlag, dir, "--cluster", cluster})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout.String(), "No reservations on") {
		t.Errorf("output does not say there is nothing to release: %s", stdout.String())
	}
}
