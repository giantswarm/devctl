package release_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/release"
	"github.com/giantswarm/devctl/v8/internal/gittest"
)

// Flag names, shared across tests that build a release command's --args.
const (
	flagRepoDir = "--repo-dir"
	flagCluster = "--cluster"
	flagApp     = "--app"
	flagUser    = "--user"

	flagPullRequest = "--pull-request"
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
		flagRepoDir, t.TempDir(),
		flagCluster, "graveler",
		flagApp, "hello-world",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !release.IsInvalidFlag(err) {
		t.Fatalf("expected an invalid-flag error, got %v", err)
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

	return newReleasableFixtureWithCharts(t, cluster, chart)
}

// newReleasableFixtureWithCharts is newReleasableFixture for one or more
// charts reserved on the same cluster, so a test can release them from
// separate clones and drive a genuine rejected push.
func newReleasableFixtureWithCharts(t *testing.T, cluster string, charts ...string) (dir, origin string) {
	t.Helper()

	origin = t.TempDir()
	gittest.RunGit(t, origin, "init", "--bare", "-b", "main")

	dir = t.TempDir()
	gittest.RunGit(t, dir, "init", "-b", "main")
	gittest.RunGit(t, dir, "remote", "add", "origin", origin)

	clusterDir := filepath.Join(dir, "management-clusters", cluster)
	collectionsDir := filepath.Join(clusterDir, "collections")

	configMap := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"data:\n"
	kustomization := "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
		"kind: Kustomization\n" +
		"resources: []\n" +
		"components:\n"

	files := map[string]string{}
	for _, chart := range charts {
		componentDir := filepath.Join(collectionsDir, "reservations", chart)
		if err := os.MkdirAll(componentDir, 0o750); err != nil {
			t.Fatal(err)
		}

		configMap += "  " + chart + ": '{user: alice, branch: fix/crash, pr: giantswarm/hello-world#123, scope: app, from: 2026-09-15T10:00:00Z, until: 2026-09-15T20:00:00Z}'\n"
		kustomization += "  - reservations/" + chart + "\n"
		files[filepath.Join(componentDir, "kustomization.yaml")] = "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nresources:\n  - " + chart + "-dev-reservation.yaml\n"
		files[filepath.Join(componentDir, chart+"-dev-reservation.yaml")] = "apiVersion: source.toolkit.fluxcd.io/v1\nkind: OCIRepository\nmetadata:\n  name: " + chart + "-dev-reservation\n  namespace: giantswarm\n"
	}
	files[filepath.Join(clusterDir, "configmap-reservations.yaml")] = configMap
	files[filepath.Join(collectionsDir, "kustomization.yaml")] = kustomization

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	gittest.RunGit(t, dir, "add", "-A")
	gittest.RunGit(t, dir, "commit", "-m", "reserve "+strings.Join(charts, ", "))
	gittest.RunGit(t, dir, "push", "-u", "origin", "main")

	return dir, origin
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
		flagRepoDir, dir,
		flagCluster, cluster,
		flagApp, chart,
		flagUser, "alice",
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

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	pushed := gittest.GitOutput(t, origin, "rev-parse", "main")
	if head != pushed {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, pushed)
	}
}

// TestReleaseRetriesAPushRejectedByAnotherRelease is the cobra-command-surface
// proof for the ticket's hard case: two `release` commands, run from separate
// clones against the same cluster, both land. The second command's push is
// rejected because the first already landed; it must rebase and rerun Release
// against the rebased tree, not just replay its stale commit.
func TestReleaseRetriesAPushRejectedByAnotherRelease(t *testing.T) {
	const cluster, chartA, chartB = "graveler", "hello-world", "other-app"
	_, origin := newReleasableFixtureWithCharts(t, cluster, chartA, chartB)

	dir1 := t.TempDir()
	gittest.RunGit(t, dir1, "clone", origin, ".")
	dir2 := t.TempDir()
	gittest.RunGit(t, dir2, "clone", origin, ".")

	// dir2 releases chartB and lands first.
	var stdout2 bytes.Buffer
	cmd2 := newCommand(t, &stdout2)
	cmd2.SetArgs([]string{
		flagRepoDir, dir2,
		flagCluster, cluster,
		flagApp, chartB,
		flagUser, "bob",
	})
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("release chartB: %v", err)
	}

	// dir1 started from the same tip as dir2 and still releases chartA against
	// it: its first push is rejected, so it must rebase and rerun Release.
	var stdout1 bytes.Buffer
	cmd1 := newCommand(t, &stdout1)
	cmd1.SetArgs([]string{
		flagRepoDir, dir1,
		flagCluster, cluster,
		flagApp, chartA,
		flagUser, "alice",
	})
	if err := cmd1.Execute(); err != nil {
		t.Fatalf("release chartA: %v", err)
	}
	if !strings.Contains(stdout1.String(), chartA) {
		t.Errorf("stdout does not mention %q: %s", chartA, stdout1.String())
	}

	head := gittest.GitOutput(t, dir1, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", "main")
	if head != tip {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, tip)
	}

	configMap := gittest.GitOutput(t, origin, "show", "main:management-clusters/"+cluster+"/configmap-reservations.yaml")
	if strings.Contains(configMap, chartA+":") {
		t.Errorf("chartA reservation still present after release: %s", configMap)
	}
	if strings.Contains(configMap, chartB+":") {
		t.Errorf("chartB reservation still present after release: %s", configMap)
	}
}

// TestReleaseByPullRequestNeedsNoClusterOrApp is the merged-pull-request
// path: the workflow that runs on a merge knows the pull request and nothing
// else, so it passes --pull-request alone and every reservation that pull
// request holds goes.
func TestReleaseByPullRequestNeedsNoClusterOrApp(t *testing.T) {
	const cluster, chartA, chartB = "graveler", "hello-world", "other-app"
	dir, origin := newReleasableFixtureWithCharts(t, cluster, chartA, chartB)

	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagPullRequest, "giantswarm/hello-world#123",
		flagUser, "alice",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("release by pull request: %v", err)
	}
	for _, chart := range []string{chartA, chartB} {
		if !strings.Contains(stdout.String(), chart) {
			t.Errorf("stdout does not mention %q: %s", chart, stdout.String())
		}
	}

	configMap := gittest.GitOutput(t, origin, "show", "main:management-clusters/"+cluster+"/configmap-reservations.yaml")
	for _, chart := range []string{chartA, chartB} {
		if strings.Contains(configMap, chart+":") {
			t.Errorf("%s reservation still present after release: %s", chart, configMap)
		}
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", "main")
	if head != tip {
		t.Errorf("push did not land: local HEAD %s, origin main %s", head, tip)
	}
}

// TestReleaseOneClusterChecksThePullRequest is the /undeploy <MC> form: it
// names a cluster and an app, and it also names the pull request it runs
// from, so a pull request cannot free a cluster another one is testing on.
func TestReleaseOneClusterChecksThePullRequest(t *testing.T) {
	const cluster, chart = "graveler", "hello-world"

	// The holder's pull request releases its own reservation.
	dir, _ := newReleasableFixture(t, cluster, chart)
	var stdout bytes.Buffer
	cmd := newCommand(t, &stdout)
	cmd.SetArgs([]string{
		flagRepoDir, dir,
		flagCluster, cluster,
		flagApp, chart,
		flagUser, "alice",
		flagPullRequest, "giantswarm/hello-world#123",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("release from the holding pull request: %v", err)
	}

	// An unrelated pull request is refused, and the reservation survives.
	other, otherOrigin := newReleasableFixture(t, cluster, chart)
	otherCmd := newCommand(t, io.Discard)
	otherCmd.SetArgs([]string{
		flagRepoDir, other,
		flagCluster, cluster,
		flagApp, chart,
		flagUser, "mallory",
		flagPullRequest, "giantswarm/hello-world#999",
	})
	if err := otherCmd.Execute(); err == nil {
		t.Fatal("expected a refusal from an unrelated pull request, got none")
	}

	configMap := gittest.GitOutput(t, otherOrigin, "show", "main:management-clusters/"+cluster+"/configmap-reservations.yaml")
	if !strings.Contains(configMap, chart+":") {
		t.Errorf("the refusal removed the reservation: %s", configMap)
	}
}
