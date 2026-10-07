package reservation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/gitsemver/v3/pkg/gitsemver"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func testRequest(dir string) reservation.Request {
	return reservation.Request{
		RepoDir:     dir,
		Cluster:     fixtureCluster,
		App:         fixtureApp,
		Branch:      testBranch,
		User:        testUser,
		PullRequest: testPullRequest,
		Now:         time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
	}
}

const (
	// the holder of the reservation under test.
	testUser        = "alice"
	testOtherUser   = "bob"
	testBranch      = "fix/crash"
	testOtherBranch = "feat/other"
	testPullRequest = "giantswarm/hello-world#123"
)

// fixture paths the reservation touches.
const (
	pathCollections  = "management-clusters/" + fixtureCluster + "/collections/kustomization.yaml"
	pathComponent    = "management-clusters/" + fixtureCluster + "/collections/reservations/" + fixtureApp + "/kustomization.yaml"
	pathSource       = "management-clusters/" + fixtureCluster + "/collections/reservations/" + fixtureApp + "/" + fixtureApp + "-dev-reservation.yaml"
	pathReservations = "management-clusters/" + fixtureCluster + "/configmap-reservations.yaml"
)

func TestReserveRefusesClusterThatIsNotEnabled(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{omitConfigMap: true})

	_, err := reservation.Reserve(testRequest(dir))
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsClusterNotEnabled(err) {
		t.Fatalf("expected a cluster-not-enabled error, got %v", err)
	}

	// The refusal has to say how to enable the cluster, or the reader is left
	// guessing which file to add and where to wire it.
	for _, want := range []string{
		"configmap-reservations.yaml",
		"management-clusters/" + fixtureCluster,
		"resources",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}

func TestReserveWritesTheReservationInOneCommit(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	req := testRequest(dir)

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{pathCollections, pathSource, pathComponent, pathReservations}
	sort.Strings(want)
	if diff := cmp.Diff(want, res.Files); diff != "" {
		t.Errorf("written files (-want +got):\n%s", diff)
	}

	if res.SourceName != fixtureApp+"-dev-reservation" {
		t.Errorf("source name: got %q", res.SourceName)
	}
	if got, want := res.Until, req.Now.Add(10*time.Hour); !got.Equal(want) {
		t.Errorf("until: got %s, want %s", got, want)
	}

	// The entry and the component have to land together: a component without an
	// entry is an unclaimed pin, an entry without a component is a lock over
	// nothing.
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if res.Commit != head.Hash().String() {
		t.Errorf("result commit %q is not HEAD %q", res.Commit, head.Hash())
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	stats, err := commit.Stats()
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for _, s := range stats {
		changed = append(changed, s.Name)
	}
	sort.Strings(changed)
	if diff := cmp.Diff(want, changed); diff != "" {
		t.Errorf("files in the commit (-want +got):\n%s", diff)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	status, err := wt.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.IsClean() {
		t.Errorf("working tree is not clean after Reserve:\n%s", status)
	}
}

// deepCopyYAML round-trips an object so a test can build the expected value by
// editing a copy of the original.
func deepCopyYAML(t *testing.T, o map[string]any) map[string]any {
	t.Helper()

	b, err := yaml.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var copied map[string]any
	if err := yaml.Unmarshal(b, &copied); err != nil {
		t.Fatal(err)
	}

	return copied
}

func TestReserveSourceIsACopyOfTheOriginal(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	req := testRequest(dir)

	before := renderCluster(t, dir, fixtureCluster)
	original := mustObject(t, before, "OCIRepository/"+fixtureApp)

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	after := renderCluster(t, dir, fixtureCluster)

	// Everything the original carries has to survive the copy. A lost secretRef
	// is a registry authentication failure that reads exactly like a missing
	// chart, so the test compares whole objects rather than a list of fields.
	want := deepCopyYAML(t, original)
	want["metadata"].(map[string]any)["name"] = res.SourceName
	want["metadata"].(map[string]any)["annotations"] = map[string]any{
		"reservation.giantswarm.io/user":   testUser,
		"reservation.giantswarm.io/branch": testBranch,
		"reservation.giantswarm.io/pr":     testPullRequest,
		"reservation.giantswarm.io/scope":  "app",
		"reservation.giantswarm.io/from":   "2026-09-15T10:00:00Z",
		"reservation.giantswarm.io/until":  "2026-09-15T20:00:00Z",
	}
	// A dev build has to land in minutes, unlike a release.
	want["spec"].(map[string]any)["interval"] = "1m"
	want["spec"].(map[string]any)["ref"] = map[string]any{
		"semver":       ">=0.0.0-0",
		"semverFilter": res.SemverFilter,
	}

	if diff := cmp.Diff(want, mustObject(t, after, "OCIRepository/"+res.SourceName)); diff != "" {
		t.Errorf("reservation source (-want +got):\n%s", diff)
	}

	// The release and release-candidate version selection has to keep working:
	// the app's own source object, and every other app on the cluster, come out
	// of the render untouched.
	for _, key := range []string{
		"OCIRepository/" + fixtureApp,
		"OCIRepository/other-app",
		"HelmRelease/other-app",
	} {
		if diff := cmp.Diff(mustObject(t, before, key), mustObject(t, after, key)); diff != "" {
			t.Errorf("%s changed (-before +after):\n%s", key, diff)
		}
	}

	// The reservation only does something if the release follows it.
	release := mustObject(t, after, "HelmRelease/"+fixtureApp)
	chartRef, _ := release["spec"].(map[string]any)["chartRef"].(map[string]any)
	if got := chartRef["name"]; got != res.SourceName {
		t.Errorf("HelmRelease chartRef name: got %v, want %q", got, res.SourceName)
	}
}

// devTag builds a dev version exactly as gitsemver does, taking the branch
// fingerprint from the library so the test cannot drift from it.
func devTag(base, branch string) string {
	return fmt.Sprintf("%s-r%st20260915100000h1a2b3c4", base, gitsemver.BranchHash(branch))
}

func TestReserveFilterMatchesRealDevTags(t *testing.T) {
	// The second branch is long, and carries a slash and hyphens. None of that
	// reaches the tag: the branch is a CRC32 fingerprint, so length and illegal
	// characters do not matter. The case stays to prove it.
	for _, branch := range []string{
		testBranch,
		"renovate/update-all-non-major-dependencies",
	} {
		t.Run(branch, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})
			req := testRequest(dir)
			req.Branch = branch

			res, err := reservation.Reserve(req)
			if err != nil {
				t.Fatal(err)
			}

			filter, err := regexp.Compile(res.SemverFilter)
			if err != nil {
				t.Fatalf("filter %q does not compile: %v", res.SemverFilter, err)
			}

			// The version base is a property of the app repo, not of the GitOps
			// repo. Every base a real app can carry has to match.
			for _, base := range []string{"1.2.3", "0.0.1", "10.11.12", "1234.5678.9012"} {
				tag := devTag(base, branch)

				// devTag writes the grammar out by hand. If that spelling ever
				// drifts from gitsemver, every assertion below tests a tag no
				// build produces, and the whole test goes quietly worthless.
				if !gitsemver.IsValidDev(tag) {
					t.Fatalf("devTag built %q, which gitsemver does not accept as a dev version", tag)
				}
				if !filter.MatchString(tag) {
					t.Errorf("filter %q does not match the dev tag %q", res.SemverFilter, tag)
				}
			}

			// A release, a release candidate and another branch's dev build must
			// not match, or the reservation would follow versions nobody asked for.
			for _, tag := range []string{
				"1.2.3",
				"1.2.3-rc.1",
				devTag("1.2.3", "some/other-branch"),
			} {
				if filter.MatchString(tag) {
					t.Errorf("filter %q matches %q, which is not a dev build of %q", res.SemverFilter, tag, branch)
				}
			}
		})
	}
}

func TestReserveRefusesARenderWithoutTheReservation(t *testing.T) {
	// A cluster-level patch that pins the chart source. Kustomize applies the
	// cluster's own patches after its components, so the reservation component
	// is silently overridden: the repo would look reserved and the cluster would
	// keep running the release.
	dir := newGitOpsFixture(t, fixtureOptions{collectionsKustomization: `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../bases/collections/demo
patches:
  - patch: |
      apiVersion: helm.toolkit.fluxcd.io/v2
      kind: HelmRelease
      metadata:
        name: ` + fixtureApp + `
        namespace: giantswarm
      spec:
        chartRef:
          name: ` + fixtureApp + `
`})

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}

	_, err = reservation.Reserve(testRequest(dir))
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsRenderAssertion(err) {
		t.Fatalf("expected a render assertion error, got %v", err)
	}

	after, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if after.Hash() != before.Hash() {
		t.Errorf("a reservation that does not survive the render was committed anyway: %s", after.Hash())
	}
}

// reservationEntries parses the cluster's reservations ConfigMap and returns the
// entry of each reserved app. It is what the later release run, and a human
// with `kubectl`, read back.
func reservationEntries(t *testing.T, dir, cluster string) map[string]map[string]string {
	t.Helper()

	var configMap struct {
		Data map[string]string `yaml:"data"`
	}
	raw := readFixtureFile(t, dir, "management-clusters/"+cluster+"/"+reservation.ConfigMapFile)
	if err := yaml.Unmarshal([]byte(raw), &configMap); err != nil {
		t.Fatalf("parsing the reservations ConfigMap of %s: %v\n%s", cluster, err, raw)
	}

	entries := map[string]map[string]string{}
	for app, entry := range configMap.Data {
		var fields map[string]string
		if err := yaml.Unmarshal([]byte(entry), &fields); err != nil {
			t.Fatalf("parsing the reservation entry of %s: %v\n%s", app, err, entry)
		}
		entries[app] = fields
	}

	return entries
}

func TestReserveWritesAnEntryThatReadsBack(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	req := testRequest(dir)
	// A branch name carrying YAML flow punctuation must not corrupt the entry of
	// every other reservation on the cluster.
	req.Branch = "fix/crash, {really}"

	if _, err := reservation.Reserve(req); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"user":   testUser,
		"branch": "fix/crash, {really}",
		"pr":     testPullRequest,
		"scope":  "app",
		"from":   "2026-09-15T10:00:00Z",
		"until":  "2026-09-15T20:00:00Z",
	}
	if diff := cmp.Diff(want, reservationEntries(t, dir, fixtureCluster)[fixtureApp]); diff != "" {
		t.Errorf("reservation entry (-want +got):\n%s", diff)
	}
}

func TestReserveHoldsTwoAppsOnOneCluster(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	first := testRequest(dir)
	if _, err := reservation.Reserve(first); err != nil {
		t.Fatal(err)
	}

	second := testRequest(dir)
	second.App = fixtureOtherApp
	second.User = testOtherUser
	second.Branch = testOtherBranch
	if _, err := reservation.Reserve(second); err != nil {
		t.Fatal(err)
	}

	entries := reservationEntries(t, dir, fixtureCluster)
	if got, want := entries[fixtureApp]["user"], testUser; got != want {
		t.Errorf("%s holder: got %q, want %q", fixtureApp, got, want)
	}
	if got, want := entries[fixtureOtherApp]["user"], testOtherUser; got != want {
		t.Errorf("other-app holder: got %q, want %q", got, want)
	}

	// Both reservations have to survive the same render.
	objects := renderCluster(t, dir, fixtureCluster)
	for _, app := range []string{fixtureApp, fixtureOtherApp} {
		release := mustObject(t, objects, "HelmRelease/"+app)
		chartRef, _ := release["spec"].(map[string]any)["chartRef"].(map[string]any)
		if got, want := chartRef["name"], app+"-dev-reservation"; got != want {
			t.Errorf("%s chartRef name: got %v, want %q", app, got, want)
		}
		mustObject(t, objects, "OCIRepository/"+app+"-dev-reservation")
	}
}

func TestReserveHoldsOneAppOnTwoClusters(t *testing.T) {
	const otherCluster = "gauss"

	dir := newGitOpsFixture(t, fixtureOptions{clusters: []string{fixtureCluster, otherCluster}})

	for _, cluster := range []string{fixtureCluster, otherCluster} {
		req := testRequest(dir)
		req.Cluster = cluster
		if _, err := reservation.Reserve(req); err != nil {
			t.Fatalf("reserving on %s: %v", cluster, err)
		}
	}

	for _, cluster := range []string{fixtureCluster, otherCluster} {
		if got := reservationEntries(t, dir, cluster)[fixtureApp]["user"]; got != testUser {
			t.Errorf("%s holder on %s: got %q", fixtureApp, cluster, got)
		}
		mustObject(t, renderCluster(t, dir, cluster), "OCIRepository/"+fixtureApp+"-dev-reservation")
	}
}

// TestReserveDoesNotCommitAnUnrelatedDirtyFile is the regression for a HIGH
// finding: Reserve (like Release and Extend) is meant to run against a
// checkout of the whole GitOps repo, at --repo-dir's own default of ".". A
// developer who runs it from a checkout that also holds their own
// in-progress edits must never have that file staged or committed under the
// reservation's commit message -- Reserve only ever wrote res.Files.
func TestReserveDoesNotCommitAnUnrelatedDirtyFile(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	const unrelated = "wip.txt"
	const wipContent = "someone else's in-progress edit\n"
	if err := os.WriteFile(filepath.Join(dir, unrelated), []byte(wipContent), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := reservation.Reserve(testRequest(dir))
	if err != nil {
		t.Fatal(err)
	}

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(plumbing.NewHash(res.Commit))
	if err != nil {
		t.Fatal(err)
	}
	stats, err := commit.Stats()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if s.Name == unrelated {
			t.Fatalf("Reserve's commit carries %s, a file it never wrote: %+v", unrelated, stats)
		}
	}

	got, err := os.ReadFile(filepath.Join(dir, unrelated))
	if err != nil {
		t.Fatalf("the unrelated file was removed from the working tree: %v", err)
	}
	if string(got) != wipContent {
		t.Errorf("unrelated file content: got %q, want %q", got, wipContent)
	}
}

func TestReserveRefusesAnAppThatIsAlreadyReserved(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}

	second := testRequest(dir)
	second.User = testOtherUser
	second.Branch = testOtherBranch

	_, err := reservation.Reserve(second)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsAlreadyReserved(err) {
		t.Fatalf("expected an already-reserved error, got %v", err)
	}
	// The refusal has to name the holder, or the second caller cannot tell who to
	// ask.
	if !strings.Contains(err.Error(), testUser) {
		t.Errorf("refusal does not name the holder: %s", err.Error())
	}
}
