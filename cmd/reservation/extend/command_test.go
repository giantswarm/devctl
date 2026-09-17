package extend_test

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

	"github.com/giantswarm/devctl/v8/cmd/reservation/extend"
	"github.com/giantswarm/devctl/v8/cmd/reservation/internal/gittest"
)

// Flag names, shared across tests that build an extend command's --args.
const (
	flagRepoDir     = "--repo-dir"
	flagPullRequest = "--pull-request"
	flagUser        = "--user"
)

// newCommand builds the extend command the way cmd/reservation does.
func newCommand(t *testing.T, stdout io.Writer) *cobra.Command {
	t.Helper()

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := extend.New(extend.Config{Logger: logger, Stderr: io.Discard, Stdout: stdout})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

const testPullRequest = "giantswarm/hello-world#123"

// newExtendableFixture builds the smallest checkout Extend needs: an enabled
// cluster with one reservation, the ConfigMap entry and the reservation's own
// source object -- Extend rewrites both, in the same commit, so a `kubectl`
// read of either one always shows the current window. It pushes to a local
// bare "origin", so a plain `git push` after Extend succeeds with no network
// or credentials, exactly as it would from a laptop.
func newExtendableFixture(t *testing.T, cluster, app, from, until string) (dir, origin string) {
	t.Helper()

	origin = t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", "main")

	dir = t.TempDir()
	gittest.RunGit(t, dir, "init", "-b", "main")
	gittest.RunGit(t, dir, "remote", "add", "origin", origin)

	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	if err := os.MkdirAll(clusterDir, 0o750); err != nil {
		t.Fatal(err)
	}

	configMap := filepath.Join(clusterDir, "configmap-reservations.yaml")
	content := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"  annotations:\n" +
		"    reservations.giantswarm.io/max-duration: 7d\n" +
		"data:\n" +
		"  " + app + ": '{user: alice, branch: fix/crash, pr: \"" + testPullRequest + "\", scope: app, from: " + from + ", until: " + until + "}'\n"
	if err := os.WriteFile(configMap, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	sourceDir := filepath.Join(clusterDir, "collections", "reservations", app)
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatal(err)
	}
	source := "apiVersion: source.toolkit.fluxcd.io/v1\n" +
		"kind: OCIRepository\n" +
		"metadata:\n" +
		"  annotations:\n" +
		"    reservation.giantswarm.io/branch: fix/crash\n" +
		"    reservation.giantswarm.io/from: \"" + from + "\"\n" +
		"    reservation.giantswarm.io/pr: \"" + testPullRequest + "\"\n" +
		"    reservation.giantswarm.io/scope: app\n" +
		"    reservation.giantswarm.io/until: \"" + until + "\"\n" +
		"    reservation.giantswarm.io/user: alice\n" +
		"  name: " + app + "-dev-reservation\n" +
		"  namespace: giantswarm\n"
	if err := os.WriteFile(filepath.Join(sourceDir, app+"-dev-reservation.yaml"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	gittest.RunGit(t, dir, "add", "-A")
	gittest.RunGit(t, dir, "commit", "-m", "reserve "+app)
	gittest.RunGit(t, dir, "push", "-u", "origin", "main")

	return dir, origin
}

// TestExtendCommitsAndPushesWithNoToken is the CLI-level walking skeleton: an
// active reservation matching --pull-request gets a fresh window, the command
// prints one tab-separated line naming cluster, app and the new expiry, and a
// plain `git push` succeeds against a local remote, with no GitHub token.
func TestExtendCommitsAndPushesWithNoToken(t *testing.T) {
	const cluster, app = "graveler", "hello-world"
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	until := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339) // still active
	dir, origin := newExtendableFixture(t, cluster, app, from, until)

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagPullRequest, testPullRequest,
		flagUser, "reservation-extend",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("extend: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	fields := strings.Split(out, "\t")
	if len(fields) != 3 {
		t.Fatalf("stdout: got %d tab-separated fields, want 3: %q", len(fields), out)
	}
	if fields[0] != cluster || fields[1] != app {
		t.Errorf("stdout fields: got %q", out)
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	pushed := gittest.GitOutput(t, origin, "rev-parse", "main")
	if head != pushed {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, pushed)
	}
}

// TestExtendRefusalNamesDeploy is the acceptance criterion "the refusal must
// name /deploy": a pull request with no matching reservation anywhere fails
// the command and the error names /deploy as the way to create one.
func TestExtendRefusalNamesDeploy(t *testing.T) {
	const cluster, app = "graveler", "hello-world"
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	until := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	dir, _ := newExtendableFixture(t, cluster, app, from, until)

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagPullRequest, "giantswarm/hello-world#999", // no reservation names this PR
		flagUser, "reservation-extend",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !strings.Contains(err.Error(), "/deploy") {
		t.Errorf("refusal does not name /deploy: %v", err)
	}
	if stdout.String() != "" {
		t.Errorf("stdout: got %q, want empty", stdout.String())
	}
}
