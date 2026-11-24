package reap_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/reap"
	"github.com/giantswarm/devctl/v8/internal/gittest"
)

// Flag names, shared across tests that build a reap command's --args.
const (
	flagRepoDir = "--gitops-dir"
	flagUser    = "--user"
)

// newCommand builds the reap command the way cmd/reservation does, and clears
// every GitHub token env var reap would otherwise pick up: a real CI runner
// often carries an ambient GITHUB_TOKEN, and these tests must never call the
// real GitHub API.
func newCommand(t *testing.T, stdout io.Writer) *cobra.Command {
	t.Helper()

	return newCommandWithStderr(t, stdout, io.Discard)
}

// newCommandWithStderr is newCommand with a stderr the test can read.
func newCommandWithStderr(t *testing.T, stdout, stderr io.Writer) *cobra.Command {
	t.Helper()

	// CI makes authstore refuse without opening the developer's keychain.
	t.Setenv("CI", "1")
	for _, key := range []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN"} {
		t.Setenv(key, "")
	}

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := reap.New(reap.Config{Logger: logger, Stderr: stderr, Stdout: stdout})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

// newReapableFixture builds the smallest checkout Reap needs on a cluster: an
// enabled cluster with one reservation already expired (or not, per until).
// It pushes to a local bare "origin", so a plain `git push` after Reap
// succeeds with no network or credentials, exactly as it would from a laptop.
func newReapableFixture(t *testing.T, cluster, app, until string) (dir, origin string) {
	t.Helper()

	origin = t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", "main")

	dir = t.TempDir()
	gittest.RunGit(t, dir, "init", "-b", "main")
	gittest.RunGit(t, dir, "remote", "add", "origin", origin)

	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	collectionsDir := filepath.Join(clusterDir, "collections")
	if err := os.MkdirAll(collectionsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		filepath.Join(clusterDir, "configmap-reservations.yaml"): "apiVersion: v1\n" +
			"kind: ConfigMap\n" +
			"metadata:\n" +
			"  name: reservations\n" +
			"  namespace: giantswarm\n" +
			"data:\n" +
			"  " + app + ": '{user: alice, branch: fix/crash, pr: \"\", scope: app, from: 2026-09-15T10:00:00Z, until: " + until + "}'\n",
		filepath.Join(collectionsDir, "kustomization.yaml"): "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
			"kind: Kustomization\n" +
			"resources: []\n" +
			"patches:\n" +
			"  # BEGIN reservation of " + app + ", written by `devctl reservation reserve`. Do not edit.\n" +
			"  # END reservation of " + app + ", written by `devctl reservation reserve`\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	gittest.RunGit(t, dir, "add", "-A")
	gittest.RunGit(t, dir, "commit", "-m", "reserve "+app)
	gittest.RunGit(t, dir, "push", "-u", "origin", "main")

	return dir, origin
}

// TestReapCommitsAndPushesWithNoToken is the CLI-level proof for "does the
// same work from a laptop": Reap releases an expired reservation, prints one
// tab-separated line describing it, commits and a plain `git push` succeeds
// against a local remote, with no GitHub token.
func TestReapCommitsAndPushesWithNoToken(t *testing.T) {
	const cluster, app = "graveler", "hello-world"
	until := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	dir, origin := newReapableFixture(t, cluster, app, until)

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagUser, "reservation-reaper",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("reap: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	fields := strings.Split(out, "\t")
	if len(fields) != 9 {
		t.Fatalf("stdout: got %d tab-separated fields, want 9: %q", len(fields), out)
	}
	wantCluster, wantApp, wantUser, wantReason := cluster, app, "alice", "expired"
	if fields[0] != wantCluster || fields[1] != wantApp || fields[2] != wantUser || fields[5] != wantReason || fields[8] != "collections" {
		t.Errorf("stdout fields: got %q", out)
	}

	if b, err := os.ReadFile(filepath.Join(dir, "management-clusters", cluster, "collections", "kustomization.yaml")); err != nil || strings.Contains(string(b), "reservation of") {
		t.Errorf("reservation block still exists: %v", err)
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	pushed := gittest.GitOutput(t, origin, "rev-parse", "main")
	if head != pushed {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, pushed)
	}
}

// TestReapStaysSilentWhenNothingToDo is the fourth acceptance criterion at the
// command surface: an enabled cluster with a reservation nowhere near expiry
// prints nothing and pushes nothing.
func TestReapStaysSilentWhenNothingToDo(t *testing.T) {
	const cluster, app = "graveler", "hello-world"
	until := time.Now().Add(10 * time.Hour).UTC().Format(time.RFC3339)
	dir, origin := newReapableFixture(t, cluster, app, until)
	beforeTip := gittest.GitOutput(t, origin, "rev-parse", "main")

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagUser, "reservation-reaper",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if stdout.String() != "" {
		t.Errorf("stdout: got %q, want empty", stdout.String())
	}

	afterTip := gittest.GitOutput(t, origin, "rev-parse", "main")
	if afterTip != beforeTip {
		t.Errorf("origin moved even though nothing was reaped: %s -> %s", beforeTip, afterTip)
	}
}

// TestReapWarnsWhenThereIsNoToken checks that skipping the rename check is
// said on stderr. CI=1 makes authstore refuse without opening the keychain,
// so a developer's stored login cannot leak into the test.
func TestReapWarnsWhenThereIsNoToken(t *testing.T) {
	t.Setenv("CI", "1")
	until := time.Now().Add(10 * time.Hour).UTC().Format(time.RFC3339)
	dir, _ := newReapableFixture(t, "graveler", "hello-world", until)

	var stdout, stderr bytes.Buffer
	cmd := newCommandWithStderr(t, &stdout, &stderr)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagUser, "reservation-reaper",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	const want = "Warning: no GitHub token found, so reap skips the rename check and releases only expired reservations. Set one of DEVCTL_GITHUB_TOKEN, GITHUB_TOKEN, OPSCTL_GITHUB_TOKEN or run devctl auth login --github-only.\n"
	if !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr: got %q, want it to contain %q", stderr.String(), want)
	}
	if stdout.String() != "" {
		t.Errorf("stdout: got %q, want empty", stdout.String())
	}
}

// TestReapDryRunPushesNothing checks that --dry-run reports the reap as JSON,
// then drops the commit: origin and the checkout stay at the old main.
func TestReapDryRunPushesNothing(t *testing.T) {
	const cluster, app = "graveler", "hello-world"
	until := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	dir, origin := newReapableFixture(t, cluster, app, until)
	before := gittest.GitOutput(t, origin, "rev-parse", "main")

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagUser, "reservation-reaper",
		"--dry-run",
		"--output", "json",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("reap: %v", err)
	}

	// A map, not a struct: the keys are what callers parse.
	var reaped []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &reaped); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if len(reaped) != 1 || reaped[0]["cluster"] != cluster || reaped[0]["app"] != app || reaped[0]["reason"] != "expired" || reaped[0]["gitopsSource"] != "collections" {
		t.Errorf("stdout: got %s", stdout.String())
	}

	if got := gittest.GitOutput(t, origin, "rev-parse", "main"); got != before {
		t.Errorf("dry run pushed: origin main %s, want %s", got, before)
	}
	if got := gittest.GitOutput(t, dir, "rev-parse", "HEAD"); got != before {
		t.Errorf("dry run kept its commit: HEAD %s, want %s", got, before)
	}
	if diff := gittest.GitOutput(t, dir, "status", "--porcelain"); diff != "" {
		t.Errorf("dry run left the reservation block removed: %s", diff)
	}
}

// TestReapJSONIsAnEmptyArrayWhenNothingToDo checks that JSON output is [],
// not null, when nothing expired.
func TestReapJSONIsAnEmptyArrayWhenNothingToDo(t *testing.T) {
	until := time.Now().Add(10 * time.Hour).UTC().Format(time.RFC3339)
	dir, _ := newReapableFixture(t, "graveler", "hello-world", until)

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{flagRepoDir, dir, flagUser, "reservation-reaper", "--output", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "[]" {
		t.Errorf("stdout: got %q, want []", got)
	}
}
