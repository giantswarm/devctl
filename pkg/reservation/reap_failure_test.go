package reservation_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func TestReapStopsAfterARefusedDirtyReset(t *testing.T) {
	const lastCluster = "zz-last"
	dir, origin := newReapFixture(t, fixtureOptions{clusters: []string{fixtureCluster, lastCluster}})
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for _, target := range []struct{ cluster, app string }{
		{fixtureCluster, fixtureApp},
		{fixtureCluster, fixtureOtherApp},
		{lastCluster, fixtureApp},
	} {
		req := testRequest(dir)
		req.Cluster, req.App = target.cluster, target.app
		req.Now = now.Add(-2 * time.Hour)
		req.Duration = time.Hour
		reserveAndPush(t, dir, req)
	}

	// Model a remote update and a local edit after the command's initial Sync.
	other := t.TempDir()
	gittest.RunGit(t, other, "clone", origin, ".")
	writeAndCommit(t, other, "unrelated.txt", "remote update\n")
	gittest.RunGit(t, other, "push", "origin", "main")
	remoteTip := gittest.GitOutput(t, origin, "rev-parse", "main")
	basePath := filepath.Join(dir, fixtureAppFile)
	base, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	wip := append([]byte("# local work in progress\n"), base...)
	if err := os.WriteFile(basePath, wip, 0o600); err != nil {
		t.Fatal(err)
	}

	reaped, err := reservation.Reap(context.Background(), reservation.ReapRequest{
		RepoDir: dir, User: testReaper, Now: now,
	})
	if !reservation.IsDirtyWorktree(err) {
		t.Fatalf("got %v, want the refused reset", err)
	}
	if len(reaped) != 0 {
		t.Errorf("got releases despite the refused reset: %+v", reaped)
	}
	if got := gittest.GitOutput(t, dir, "rev-list", "--count", "origin/main..HEAD"); got != "1" {
		t.Errorf("sweep kept committing after the refused reset: %s commits, want 1", got)
	}
	if got := gittest.GitOutput(t, origin, "rev-parse", "main"); got != remoteTip {
		t.Errorf("origin moved after the refused reset: %s, want %s", got, remoteTip)
	}
	if got := readFixtureFile(t, dir, fixtureAppFile); got != string(wip) {
		t.Error("the unrelated local edit changed")
	}
	remaining, listErr := reservation.List(reservation.ListRequest{RepoDir: dir, Cluster: fixtureCluster})
	if listErr != nil || len(remaining) != 1 || remaining[0].App != fixtureOtherApp {
		t.Errorf("the next app was touched: remaining %+v, error %v", remaining, listErr)
	}
	remaining, listErr = reservation.List(reservation.ListRequest{RepoDir: dir, Cluster: lastCluster})
	if listErr != nil || len(remaining) != 1 {
		t.Errorf("the next cluster was touched: remaining %+v, error %v", remaining, listErr)
	}
}
