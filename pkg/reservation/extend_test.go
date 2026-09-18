package reservation_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const testExtender = "reservation-extend"

// TestExtendRefusesWhenNothingMatches is the walking skeleton: an enabled
// cluster holding no reservation at all refuses, and the refusal names
// /deploy as the way to create one.
func TestExtendRefusesWhenNothingMatches(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsNothingToExtend(err) {
		t.Fatalf("expected a nothing-to-extend error, got %v", err)
	}
	if !strings.Contains(err.Error(), "/deploy") {
		t.Errorf("refusal does not name /deploy: %s", err.Error())
	}
	if len(extended) != 0 {
		t.Errorf("got %d extended reservations, want 0: %+v", len(extended), extended)
	}
}

// TestExtendResetsTheExpiryOfAMatchingReservation is the first acceptance
// criterion: a single active reservation matching the pull request gets a
// fresh window that starts now and keeps its own stored duration, and the
// new window lands in the ConfigMap entry and is pushed.
func TestExtendResetsTheExpiryOfAMatchingReservation(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 4 * time.Hour
	reserveAndPush(t, dir, req)

	now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC) // before the original 14:00 expiry
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if len(extended) != 1 {
		t.Fatalf("got %d extended reservations, want 1: %+v", len(extended), extended)
	}

	got := extended[0]
	wantUntil := now.Add(4 * time.Hour) // the original 4h duration, not reset to the default
	if got.Cluster != fixtureCluster || got.App != fixtureApp || !got.From.Equal(now) || !got.Until.Equal(wantUntil) {
		t.Errorf("extended entry: %+v, want From %s Until %s", got, now, wantUntil)
	}

	entry := reservationEntries(t, dir, fixtureCluster)[fixtureApp]
	if got := entry["from"]; got != now.Format(time.RFC3339) {
		t.Errorf("entry from: got %q, want %q", got, now.Format(time.RFC3339))
	}
	if got := entry["until"]; got != wantUntil.Format(time.RFC3339) {
		t.Errorf("entry until: got %q, want %q", got, wantUntil.Format(time.RFC3339))
	}
	// Everything else about the reservation stays put.
	if got := entry["user"]; got != testUser {
		t.Errorf("entry user: got %q, want %q", got, testUser)
	}
	if got := entry["branch"]; got != testBranch {
		t.Errorf("entry branch: got %q, want %q", got, testBranch)
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the extension did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}

// TestExtendUpdatesTheReservationSourceAnnotations is the spec's own
// end-to-end check (reservation-flow-spec.md line 543): an engineer with no
// access to the GitOps repo reads /from and /until off the reservation
// OCIRepository with kubectl, so Extend has to move them there too, in the
// same commit as the ConfigMap entry. The holder's own annotations never
// change -- Extend only moves the window.
func TestExtendUpdatesTheReservationSourceAnnotations(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 4 * time.Hour
	result := reserveAndPush(t, dir, req)

	now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC) // before the original 14:00 expiry
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if len(extended) != 1 {
		t.Fatalf("got %d extended reservations, want 1: %+v", len(extended), extended)
	}
	wantUntil := now.Add(4 * time.Hour) // the original 4h duration, not reset to the default

	source := mustObject(t, renderCluster(t, dir, fixtureCluster), "OCIRepository/"+result.SourceName)
	annotations, _ := source["metadata"].(map[string]any)["annotations"].(map[string]any)
	if got, want := annotations[reservation.AnnotationPrefix+"from"], now.Format(time.RFC3339); got != want {
		t.Errorf("annotation from: got %v, want %q", got, want)
	}
	if got, want := annotations[reservation.AnnotationPrefix+"until"], wantUntil.Format(time.RFC3339); got != want {
		t.Errorf("annotation until: got %v, want %q", got, want)
	}
	// Everything that names the holder stays exactly as it was.
	if got := annotations[reservation.AnnotationPrefix+"user"]; got != testUser {
		t.Errorf("annotation user: got %v, want %q", got, testUser)
	}
	if got := annotations[reservation.AnnotationPrefix+"branch"]; got != testBranch {
		t.Errorf("annotation branch: got %v, want %q", got, testBranch)
	}
	if got := annotations[reservation.AnnotationPrefix+"pr"]; got != testPullRequest {
		t.Errorf("annotation pr: got %v, want %q", got, testPullRequest)
	}
	if got := annotations[reservation.AnnotationPrefix+"scope"]; got != reservation.ScopeApp {
		t.Errorf("annotation scope: got %v, want %q", got, reservation.ScopeApp)
	}
}

// TestExtendIgnoresAnExpiredReservation is the MANDATORY rule: a record whose
// Until has already passed is treated as if it did not exist at all, even
// though it still names the pull request, because reviving it could silently
// break an exclusive lock someone else legally took over the same cluster
// while the dead record sat unswept. Extend refuses exactly as if it found
// nothing, and never pushes.
func TestExtendIgnoresAnExpiredReservation(t *testing.T) {
	dir, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = time.Hour
	reserveAndPush(t, dir, req)
	beforeTip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))

	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC), // 1h past the 11:00 expiry
	})
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsNothingToExtend(err) {
		t.Fatalf("expected a nothing-to-extend error, got %v", err)
	}
	if len(extended) != 0 {
		t.Errorf("got %d extended reservations, want 0: %+v", len(extended), extended)
	}

	afterTip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if afterTip != beforeTip {
		t.Errorf("origin moved even though nothing was extended: %s -> %s", beforeTip, afterTip)
	}
}

// TestExtendResetsEveryMatchingReservation is the acceptance criterion "extend
// EVERY match, not just the first": a pull request holding reservations on two
// different apps on the same cluster gets both reset, not only the one List
// happens to return first.
func TestExtendResetsEveryMatchingReservation(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	first := testRequest(dir)
	first.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	first.Duration = 4 * time.Hour
	reserveAndPush(t, dir, first)

	second := testRequest(dir)
	second.App = fixtureOtherApp
	second.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	second.Duration = 2 * time.Hour
	reserveAndPush(t, dir, second)

	now := time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC) // before both expiries
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if len(extended) != 2 {
		t.Fatalf("got %d extended reservations, want 2: %+v", len(extended), extended)
	}

	byApp := map[string]reservation.Extended{}
	for _, e := range extended {
		byApp[e.App] = e
	}
	if got := byApp[fixtureApp]; !got.Until.Equal(now.Add(4 * time.Hour)) {
		t.Errorf("%s until: got %+v, want Until %s", fixtureApp, got, now.Add(4*time.Hour))
	}
	if got := byApp[fixtureOtherApp]; !got.Until.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("%s until: got %+v, want Until %s", fixtureOtherApp, got, now.Add(2*time.Hour))
	}

	entries := reservationEntries(t, dir, fixtureCluster)
	if entries[fixtureApp]["from"] != now.Format(time.RFC3339) {
		t.Errorf("%s entry not reset: %+v", fixtureApp, entries[fixtureApp])
	}
	if entries[fixtureOtherApp]["from"] != now.Format(time.RFC3339) {
		t.Errorf("%s entry not reset: %+v", fixtureOtherApp, entries[fixtureOtherApp])
	}
}

// TestExtendRefusesOneReservationOverTheLoweredCap is the acceptance criterion
// that a single reservation whose stored duration now exceeds its cluster's
// cap is refused on its own -- one failing cluster (or record) must not stop
// the sweep, and Extend must still return what did succeed rather than a bare
// nil, err.
func TestExtendRefusesOneReservationOverTheLoweredCap(t *testing.T) {
	dir, _ := newReapFixture(t, fixtureOptions{})

	// Reserved while the cap still allowed it.
	first := testRequest(dir)
	first.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	first.Duration = 4 * time.Hour
	reserveAndPush(t, dir, first)

	second := testRequest(dir)
	second.App = fixtureOtherApp
	second.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	second.Duration = 2 * time.Hour // still unexpired, and exactly at the lowered cap below
	reserveAndPush(t, dir, second)

	// The cluster's owners lower the cap after the fact, below the first
	// reservation's already-stored 4h but still at or above the second's 2h.
	configMapPath := filepath.Join(dir, "management-clusters", fixtureCluster, "configmap-reservations.yaml")
	content, err := os.ReadFile(configMapPath) //nolint:gosec // a test fixture
	if err != nil {
		t.Fatal(err)
	}
	lowered := strings.Replace(string(content), "max-duration: 7d", "max-duration: 2h", 1)
	if lowered == string(content) {
		t.Fatal("fixture no longer carries the max-duration annotation this test rewrites")
	}
	if err := os.WriteFile(configMapPath, []byte(lowered), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC) // before both expiries
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err == nil {
		t.Fatal("expected an error reporting the over-cap reservation, got none")
	}
	if !reservation.IsInvalidDuration(err) {
		t.Fatalf("expected an invalid-duration error, got %v", err)
	}
	if len(extended) != 1 || extended[0].App != fixtureOtherApp {
		t.Fatalf("got %+v, want only %s extended despite %s being over the lowered cap", extended, fixtureOtherApp, fixtureApp)
	}

	entries := reservationEntries(t, dir, fixtureCluster)
	if entries[fixtureApp]["from"] != first.Now.Format(time.RFC3339) {
		t.Errorf("%s was extended despite exceeding the lowered cap: %+v", fixtureApp, entries[fixtureApp])
	}
	if entries[fixtureOtherApp]["from"] != now.Format(time.RFC3339) {
		t.Errorf("%s entry not reset: %+v", fixtureOtherApp, entries[fixtureOtherApp])
	}
}

// TestExtendContinuesAfterABrokenCluster is the acceptance criterion "one
// failing cluster must not stop the sweep": a cluster whose ConfigMap fails to
// parse costs its own error, but another cluster's matching reservation is
// still extended and pushed, and the broken cluster's failure comes back for
// the caller to report -- Extend never returns a bare nil, err when some
// reservations did get extended.
func TestExtendContinuesAfterABrokenCluster(t *testing.T) {
	const brokenCluster = "broken-cluster"

	dir, origin := newReapFixture(t, fixtureOptions{clusters: []string{fixtureCluster, brokenCluster}})

	req := testRequest(dir)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 4 * time.Hour
	reserveAndPush(t, dir, req)

	// Corrupt the broken cluster's ConfigMap so List fails on it. It need not be
	// committed -- Extend reads the working tree directly, exactly as Reap does.
	brokenConfigMap := filepath.Join(dir, "management-clusters", brokenCluster, "configmap-reservations.yaml")
	corrupt := "apiVersion: v1\n" +
		"kind: ConfigMap\n" +
		"metadata:\n" +
		"  name: reservations\n" +
		"  namespace: giantswarm\n" +
		"data:\n" +
		"  broken-app: '{user: mallory, branch: x, pr: \"\", scope: app, from: not-a-date, until: not-a-date}'\n"
	if err := os.WriteFile(brokenConfigMap, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC) // before the 14:00 expiry
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err == nil {
		t.Fatal("expected an error reporting the broken cluster, got none")
	}
	if len(extended) != 1 || extended[0].Cluster != fixtureCluster {
		t.Fatalf("got %+v, want the enabled cluster's extension despite the broken one", extended)
	}

	head := gittest.GitOutput(t, dir, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the good cluster's extension did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}

// TestExtendUsesThePostRebaseReservationNotTheStaleSnapshot is the race
// PushWithRetry exists for: Extend lists a reservation, but before its own
// push lands, the SAME reservation changes underneath it -- released and
// re-reserved with a new branch and a much shorter duration, exactly as a
// developer force-pushing a rename while the extend was in flight would do.
// Extend's own push is rejected, PushWithRetry rebases onto the new tip and
// reruns render, and render must write what is on disk now -- the new branch,
// the new duration -- not the snapshot List returned before the race. A
// render that still writes the stale snapshot both resets the wrong window
// and desyncs the ConfigMap entry from the OCIRepository it commits alongside
// unchanged.
func TestExtendUsesThePostRebaseReservationNotTheStaleSnapshot(t *testing.T) {
	dir1, origin := newReapFixture(t, fixtureOptions{})

	req := testRequest(dir1)
	req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	req.Duration = 4 * time.Hour
	reserveAndPush(t, dir1, req)

	dir2 := t.TempDir()
	gittest.RunGit(t, dir2, "clone", origin, ".")

	// Before dir1's extend push lands, the reservation dir1 listed is replaced:
	// same pull request and app, but a new branch and a much shorter duration.
	if _, err := reservation.Release(reservation.ReleaseRequest{
		RepoDir: dir2, Cluster: fixtureCluster, App: fixtureApp, User: testUser,
	}); err != nil {
		t.Fatalf("releasing on dir2: %v", err)
	}
	second := testRequest(dir2)
	second.Branch = "fix/crash-v2"
	second.Now = time.Date(2026, 9, 15, 11, 0, 0, 0, time.UTC)
	second.Duration = time.Hour
	if _, err := reservation.Reserve(second); err != nil {
		t.Fatalf("re-reserving on dir2: %v", err)
	}
	if err := reservation.Push(context.Background(), dir2); err != nil {
		t.Fatalf("pushing dir2: %v", err)
	}

	// dir1's own working tree has not fetched dir2's change: it still only
	// knows the original 4h reservation, so its first push attempt is rejected
	// and PushWithRetry must rebase and rerun render.
	now := time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC)
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir:     dir1,
		PullRequest: testPullRequest,
		User:        testExtender,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if len(extended) != 1 {
		t.Fatalf("got %d extended reservations, want 1: %+v", len(extended), extended)
	}

	wantUntil := now.Add(time.Hour) // dir2's 1h duration, not the stale 4h
	if got := extended[0]; !got.Until.Equal(wantUntil) {
		t.Errorf("Until: got %s, want %s (the post-rebase 1h duration, not the stale 4h snapshot)", got.Until, wantUntil)
	}

	entries := reservationEntries(t, dir1, fixtureCluster)
	entry := entries[fixtureApp]
	if entry["branch"] != "fix/crash-v2" {
		t.Errorf("entry branch: got %q, want %q (dir2's landed branch must survive the extend, not the stale one)", entry["branch"], "fix/crash-v2")
	}
	if entry["until"] != wantUntil.Format(time.RFC3339) {
		t.Errorf("entry until: got %q, want %q", entry["until"], wantUntil.Format(time.RFC3339))
	}

	head := gittest.GitOutput(t, dir1, "rev-parse", "HEAD")
	tip := gittest.GitOutput(t, origin, "rev-parse", gittest.GitOutput(t, dir1, "branch", "--show-current"))
	if head != tip {
		t.Errorf("the extension did not land on origin: local HEAD %s, origin %s", head, tip)
	}
}
