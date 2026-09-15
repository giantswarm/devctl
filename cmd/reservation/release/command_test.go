package release_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/release"
)

// newCommand builds the release command the way cmd/reservation does.
func newCommand(t *testing.T, stdout io.Writer) *cobra.Command {
	t.Helper()

	logger, err := micrologger.New(micrologger.Config{IOWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := release.New(release.Config{Logger: logger, Stderr: io.Discard, Stdout: stdout})
	if err != nil {
		t.Fatal(err)
	}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	return cmd
}

// TestRefusesAMissingUser checks that a missing --user is refused by the
// flags, before the command ever opens --repo-dir.
func TestRefusesAMissingUser(t *testing.T) {
	cmd := newCommand(t, io.Discard)
	cmd.SetArgs([]string{
		"--repo-dir", t.TempDir(),
		"--cluster", "graveler",
		"--app", "hello-world",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !release.IsInvalidFlag(err) {
		t.Fatalf("expected an invalid-flag error, got %v", err)
	}
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

// newReleasableFixture builds the smallest checkout Release needs: a
// reservations ConfigMap holding one entry for chart, and the Kustomize
// component Reserve would have written for it. It pushes the initial commit to
// a local bare "origin", so a plain `git push` after Release succeeds without
// any network or credentials, exactly as it would tracking a real remote from
// a laptop.
func newReleasableFixture(t *testing.T, cluster, chart string) (dir, origin string) {
	t.Helper()

	origin = t.TempDir()
	runGit(t, origin, "init", "--bare", "-b", "main")

	dir = t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "remote", "add", "origin", origin)

	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	collectionsDir := filepath.Join(clusterDir, "collections")
	componentDir := filepath.Join(collectionsDir, "reservations", chart)
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
			"  " + chart + ": '{user: alice, branch: fix/crash, pr: giantswarm/hello-world#123, scope: app, from: 2026-09-15T10:00:00Z, until: 2026-09-15T20:00:00Z}'\n",
		filepath.Join(collectionsDir, "kustomization.yaml"): "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
			"kind: Kustomization\n" +
			"resources: []\n" +
			"components:\n" +
			"  - reservations/" + chart + "\n",
		filepath.Join(componentDir, "kustomization.yaml"):                    "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nresources:\n  - " + chart + "-dev-reservation.yaml\n",
		filepath.Join(componentDir, chart+"-dev-reservation.yaml"): "apiVersion: source.toolkit.fluxcd.io/v1\nkind: OCIRepository\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "reserve "+chart)
	runGit(t, dir, "push", "-u", "origin", "main")

	return dir, origin
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

// TestReleaseCommitsAndPushesWithNoToken is the CLI-level proof for "both
// commands work from a laptop": Release commits and a plain `git push`
// succeeds against a local remote, with no GitHub token and no clone.
func TestReleaseCommitsAndPushesWithNoToken(t *testing.T) {
	const cluster, chart = "graveler", "hello-world"
	dir, origin := newReleasableFixture(t, cluster, chart)

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		"--repo-dir", dir,
		"--cluster", cluster,
		"--app", chart,
		"--user", "alice",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !strings.Contains(stdout.String(), chart) {
		t.Errorf("stdout does not mention %q: %s", chart, stdout.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "management-clusters", cluster, "collections", "reservations", chart)); !os.IsNotExist(err) {
		t.Errorf("component directory still exists: %v", err)
	}

	head := gitOutput(t, dir, "rev-parse", "HEAD")
	pushed := gitOutput(t, origin, "rev-parse", "main")
	if head != pushed {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, pushed)
	}
}
