package reap_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/reap"
)

// Flag names, shared across tests that build a reap command's --args.
const (
	flagRepoDir = "--repo-dir"
	flagUser    = "--user"
)

// newCommand builds the reap command the way cmd/reservation does, and clears
// every GitHub token env var reap would otherwise pick up: a real CI runner
// often carries an ambient GITHUB_TOKEN, and these tests must never call the
// real GitHub API.
func newCommand(t *testing.T, stdout io.Writer) *cobra.Command {
	t.Helper()

	for _, key := range []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN"} {
		t.Setenv(key, "")
	}

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := reap.New(reap.Config{Logger: logger, Stderr: io.Discard, Stdout: stdout})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

// runGit runs git in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// gitOutput runs git in dir and returns its trimmed stdout, failing the test
// on error.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test-only, fixed args
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out))
}

// newReapableFixture builds the smallest checkout Reap needs on a cluster: an
// enabled cluster with one reservation already expired (or not, per until).
// It pushes to a local bare "origin", so a plain `git push` after Reap
// succeeds with no network or credentials, exactly as it would from a laptop.
func newReapableFixture(t *testing.T, cluster, app, until string) (dir, origin string) {
	t.Helper()

	origin = t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")

	dir = t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "remote", "add", "origin", origin)

	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	collectionsDir := filepath.Join(clusterDir, "collections")
	componentDir := filepath.Join(collectionsDir, "reservations", app)
	if err := os.MkdirAll(componentDir, 0o750); err != nil {
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
			"components:\n" +
			"  - reservations/" + app + "\n",
		filepath.Join(componentDir, "kustomization.yaml"):        "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nresources:\n  - " + app + "-dev-reservation.yaml\n",
		filepath.Join(componentDir, app+"-dev-reservation.yaml"): "apiVersion: source.toolkit.fluxcd.io/v1\nkind: OCIRepository\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "reserve "+app)
	runGit(t, dir, "push", "-u", "origin", "main")

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
	if len(fields) != 8 {
		t.Fatalf("stdout: got %d tab-separated fields, want 8: %q", len(fields), out)
	}
	wantCluster, wantApp, wantUser, wantReason := cluster, app, "alice", "expired"
	if fields[0] != wantCluster || fields[1] != wantApp || fields[2] != wantUser || fields[5] != wantReason {
		t.Errorf("stdout fields: got %q", out)
	}

	if _, err := os.Stat(filepath.Join(dir, "management-clusters", cluster, "collections", "reservations", app)); !os.IsNotExist(err) {
		t.Errorf("component directory still exists: %v", err)
	}

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	pushed := gitOutput(t, origin, "rev-parse", "main")
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
	beforeTip := gitOutput(t, origin, "rev-parse", "main")

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

	afterTip := gitOutput(t, origin, "rev-parse", "main")
	if afterTip != beforeTip {
		t.Errorf("origin moved even though nothing was reaped: %s -> %s", beforeTip, afterTip)
	}
}
