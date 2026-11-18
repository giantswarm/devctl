package reservation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestListShowsAnExtrasReservation: list reads the ConfigMap, which holds an
// extras reservation the same way as a collection one.
func TestListShowsAnExtrasReservation(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}

	got, err := reservation.List(testListRequest(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].App != fixtureExtrasApp || got[0].PullRequest != "giantswarm/extras-app#7" {
		t.Errorf("got %+v, want the extras-app reservation", got)
	}
}

// TestReapRestoresAnExpiredExtrasReservation: expiry restores the repo as it
// was before the reserve.
func TestReapRestoresAnExpiredExtrasReservation(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})
	original := readFixtureFile(t, dir, pathExtras)

	req := extrasRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir,
		User:    testReaper,
		Now:     time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(reaped) != 1 || reaped[0].App != fixtureExtrasApp {
		t.Fatalf("got %+v, want extras-app reaped", reaped)
	}
	if got := readFixtureFile(t, dir, pathExtras); got != original {
		t.Errorf("the reap did not restore the extras:\n%s", got)
	}
	if entries := reservationEntries(t, dir, fixtureCluster); entries[fixtureExtrasApp] != nil {
		t.Errorf("the entry is still there: %+v", entries[fixtureExtrasApp])
	}
	assertClean(t, dir)
}

// TestReleaseFromThePullRequestRestoresTheExtras is the release on a merged or
// closed pull request: the bot names the pull request that holds it.
func TestReleaseFromThePullRequestRestoresTheExtras(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	original := readFixtureFile(t, dir, pathExtras)
	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}

	req := extrasReleaseRequest(dir)
	req.PullRequest = "giantswarm/extras-app#7"
	if _, err := reservation.Release(req); err != nil {
		t.Fatal(err)
	}
	if got := readFixtureFile(t, dir, pathExtras); got != original {
		t.Errorf("the release did not restore the extras:\n%s", got)
	}
}

// TestTwoExtrasReservationsCoexist: each reservation owns one block, and a
// release removes only its own.
func TestTwoExtrasReservationsCoexist(t *testing.T) {
	dir := extrasWith(t, extrasSource("valkey", "flux-giantswarm", "valkey")+
		extrasRelease("valkey", "flux-giantswarm", "valkey", ""))
	original := readFixtureFile(t, dir, pathExtras)

	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}
	valkey := testRequest(dir)
	valkey.App = "valkey"
	valkey.PullRequest = "giantswarm/valkey-app#3"
	if _, err := reservation.Reserve(valkey); err != nil {
		t.Fatal(err)
	}

	content := readFixtureFile(t, dir, pathExtras)
	first := strings.Index(content, "BEGIN reservation of "+fixtureExtrasApp)
	second := strings.Index(content, "BEGIN reservation of valkey")
	if first < 0 || second < first {
		t.Fatalf("want the extras-app block, then the valkey block:\n%s", content)
	}

	if _, err := reservation.Release(extrasReleaseRequest(dir)); err != nil {
		t.Fatal(err)
	}
	content = readFixtureFile(t, dir, pathExtras)
	if strings.Contains(content, "BEGIN reservation of "+fixtureExtrasApp) || !strings.Contains(content, "BEGIN reservation of valkey") {
		t.Fatalf("the release of extras-app must keep only the valkey block:\n%s", content)
	}
	after := renderExtras(t, dir, fixtureCluster)
	if got := after["OCIRepository/valkey"]; got == nil {
		t.Fatal("the render lost the valkey source")
	}

	release := testReleaseRequest(dir)
	release.App = "valkey"
	if _, err := reservation.Release(release); err != nil {
		t.Fatal(err)
	}
	if got := readFixtureFile(t, dir, pathExtras); got != original {
		t.Errorf("two releases did not restore the extras:\n%s", got)
	}
}

// TestExtendRewritesTheExtrasBlock: /extend moves the window in the block and
// in the ConfigMap entry, in one commit.
func TestExtendRewritesTheExtrasBlock(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	req := extrasRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 4 * time.Hour
	reserveAndPush(t, dir, req)

	now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC)
	if _, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: req.PullRequest,
		User:        testExtender,
		Now:         now,
	}); err != nil {
		t.Fatalf("Extend: %v", err)
	}

	until := now.Add(4 * time.Hour).Format(time.RFC3339)
	if got := reservationEntries(t, dir, fixtureCluster)[fixtureExtrasApp]["until"]; got != until {
		t.Errorf("entry until: got %q, want %q", got, until)
	}
	want := map[string]string{
		reservation.AnnotationPrefix + "user":  testExtender,
		reservation.AnnotationPrefix + "from":  now.Format(time.RFC3339),
		reservation.AnnotationPrefix + "until": until,
	}
	annotations, _ := renderExtras(t, dir, fixtureCluster)["OCIRepository/"+fixtureExtrasApp]["metadata"].(map[string]any)["annotations"].(map[string]any)
	for k, v := range want {
		if annotations[k] != v {
			t.Errorf("annotation %s: got %v, want %q", k, annotations[k], v)
		}
	}
	if _, files := headFiles(t, dir); len(files) != 2 || files[1] != pathExtras {
		t.Errorf("the extend commit changed %v, want the ConfigMap and %s", files, pathExtras)
	}
}

// TestExclusiveLocksBothTrees: an exclusive reservation in one tree locks the
// charts of the other.
func TestExclusiveLocksBothTrees(t *testing.T) {
	for name, tc := range map[string]struct {
		first, second func(string) reservation.Request
	}{
		"collections first": {testRequest, extrasRequest},
		"extras first":      {extrasRequest, testRequest},
	} {
		t.Run(name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})
			exclusive := tc.first(dir)
			exclusive.Scope = reservation.ScopeExclusive
			if _, err := reservation.Reserve(exclusive); err != nil {
				t.Fatal(err)
			}
			other := tc.second(dir)
			other.User = "bob"

			if _, err := reservation.Reserve(other); !reservation.IsClusterLocked(err) {
				t.Errorf("expected a cluster-locked error, got %v", err)
			}
		})
	}
}
