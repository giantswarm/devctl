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

func TestExtendStopsAfterPushFailureFollowingAnEarlierRefusal(t *testing.T) {
	const laterCluster = "later-cluster"
	dir, origin := newReapFixture(t, fixtureOptions{clusters: []string{fixtureCluster, laterCluster}})
	from := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	first := testRequest(dir)
	first.Now = from
	first.Duration = 4 * time.Hour
	reserveAndPush(t, dir, first)
	second := first
	second.App = fixtureOtherApp
	second.Duration = 2 * time.Hour
	reserveAndPush(t, dir, second)
	later := second
	later.Cluster = laterCluster
	reserveAndPush(t, dir, later)

	// The first app now exceeds the cap, while the second may be extended.
	configMap := filepath.Join(dir, "management-clusters", fixtureCluster, "configmap-reservations.yaml")
	content := readFixtureFile(t, dir, filepath.Join("management-clusters", fixtureCluster, "configmap-reservations.yaml"))
	lowered := strings.Replace(content, "max-duration: 7d", "max-duration: 2h", 1)
	if lowered == content {
		t.Fatal("fixture has no maximum duration to lower")
	}
	if err := os.WriteFile(configMap, []byte(lowered), 0o600); err != nil {
		t.Fatal(err)
	}
	gittest.RunGit(t, dir, "add", "--", configMap)
	gittest.RunGit(t, dir, "commit", "-m", "lower the first cluster's cap")
	gittest.RunGit(t, dir, "push", "origin", "main")
	before := gittest.GitOutput(t, origin, "rev-parse", "main")

	// Only the first push fails. Continuing the sweep would let the next push
	// silently publish this failed extension together with the later cluster's.
	hook := "#!/bin/sh\nrm -- \"$0\"\necho 'simulated transport failure' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-push"), []byte(hook), 0o755); err != nil { //nolint:gosec // executable test hook
		t.Fatal(err)
	}
	extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
		RepoDir: dir, PullRequest: testPullRequest, User: testExtender, Now: from.Add(time.Hour),
	})
	if !reservation.IsPushFailed(err) {
		t.Errorf("push failure was hidden by the earlier duration refusal: %v", err)
	}
	if len(extended) != 0 {
		t.Errorf("sweep continued after a failed push: %+v", extended)
	}
	if got := gittest.GitOutput(t, origin, "rev-parse", "main"); got != before {
		t.Errorf("a later push published the failed extension: origin moved from %s to %s", before, got)
	}
	entry := reservationEntries(t, dir, laterCluster)[fixtureOtherApp]
	if got := entry["from"]; got != from.Format(time.RFC3339) {
		t.Errorf("later cluster was extended after the failed push: from %q", got)
	}
	if got := gittest.GitOutput(t, dir, "rev-list", "--count", "origin/main..HEAD"); got != "1" {
		t.Errorf("failed commit was not retained for recovery: %s local commits, want 1", got)
	}
}
