package reservation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestReserveRecordsTheGitOpsSource: the ConfigMap entry names where the
// reservation lives, so a human knows which files to look at.
func TestReserveRecordsTheGitOpsSource(t *testing.T) {
	for name, tc := range map[string]struct {
		request func(string) reservation.Request
		app     string
		want    string
	}{
		"collections": {testRequest, fixtureApp, reservation.GitOpsSourceCollections},
		"extras":      {extrasRequest, fixtureExtrasApp, reservation.GitOpsSourceExtras},
	} {
		t.Run(name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})
			res, err := reservation.Reserve(tc.request(dir))
			if err != nil {
				t.Fatal(err)
			}

			if got := reservationEntries(t, dir, fixtureCluster)[tc.app]["gitops-source"]; got != tc.want {
				t.Errorf("entry gitops-source: got %q, want %q", got, tc.want)
			}
			if res.GitOpsSource != tc.want {
				t.Errorf("result gitops source: got %q, want %q", res.GitOpsSource, tc.want)
			}
		})
	}
}

// setGitOpsSource replaces the gitops-source of app's entry by hand, commits
// and pushes it: the entry no longer matches the files.
func setGitOpsSource(t *testing.T, dir, app, from, to string) {
	t.Helper()

	content := readFixtureFile(t, dir, pathReservations)
	old := "gitops-source: " + from
	if strings.Count(content, old) != 1 {
		t.Fatalf("expected exactly one %q in %s:\n%s", old, pathReservations, content)
	}
	replacement := ""
	if to != "" {
		replacement = "gitops-source: " + to
	}
	content = strings.Replace(content, old, replacement, 1)
	content = strings.Replace(content, ", , ", ", ", 1)
	writeLocalFixtureFile(t, dir, pathReservations, content)
	gittest.RunGit(t, dir, "commit", "-am", "edit the gitops-source of "+app)
	if err := reservation.Push(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}

// TestExtendWritesTheGitOpsSourceOfTheFiles: the files win. Extend writes their
// GitOps source into the entry, over a wrong one or an absent one.
func TestExtendWritesTheGitOpsSourceOfTheFiles(t *testing.T) {
	for name, recorded := range map[string]string{
		"wrong":  reservation.GitOpsSourceCollections,
		"absent": "",
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newReapFixture(t, fixtureOptions{})
			req := extrasRequest(dir)
			req.Now = time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
			reserveAndPush(t, dir, req)
			setGitOpsSource(t, dir, fixtureExtrasApp, reservation.GitOpsSourceExtras, recorded)

			extended, err := reservation.Extend(context.Background(), reservation.ExtendRequest{
				RepoDir:     dir,
				PullRequest: req.PullRequest,
				User:        testExtender,
				Now:         req.Now.Add(time.Hour),
			})
			if err != nil {
				t.Fatalf("Extend: %v", err)
			}

			if got := reservationEntries(t, dir, fixtureCluster)[fixtureExtrasApp]["gitops-source"]; got != reservation.GitOpsSourceExtras {
				t.Errorf("entry gitops-source: got %q, want %q", got, reservation.GitOpsSourceExtras)
			}
			if len(extended) != 1 || extended[0].GitOpsSource != reservation.GitOpsSourceExtras {
				t.Errorf("extended: got %+v, want one with gitops source %q", extended, reservation.GitOpsSourceExtras)
			}
		})
	}
}

// TestListShowsTheGitOpsSourceOfTheFiles: list finds the GitOps source in the
// files, and also returns the recorded one, so the caller can warn when the
// two disagree.
func TestListShowsTheGitOpsSourceOfTheFiles(t *testing.T) {
	for name, tc := range map[string]struct {
		request  func(string) reservation.Request
		app      string
		recorded string
		edit     bool
		want     string
	}{
		"collections":        {testRequest, fixtureApp, reservation.GitOpsSourceCollections, false, reservation.GitOpsSourceCollections},
		"extras":             {extrasRequest, fixtureExtrasApp, reservation.GitOpsSourceExtras, false, reservation.GitOpsSourceExtras},
		"extras, absent":     {extrasRequest, fixtureExtrasApp, "", true, reservation.GitOpsSourceExtras},
		"extras, wrong":      {extrasRequest, fixtureExtrasApp, reservation.GitOpsSourceCollections, true, reservation.GitOpsSourceExtras},
		"collections, wrong": {testRequest, fixtureApp, reservation.GitOpsSourceExtras, true, reservation.GitOpsSourceCollections},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newReapFixture(t, fixtureOptions{})
			reserveAndPush(t, dir, tc.request(dir))
			if tc.edit {
				setGitOpsSource(t, dir, tc.app, tc.want, tc.recorded)
			}

			got, err := reservation.List(testListRequest(dir))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d reservations, want 1: %+v", len(got), got)
			}
			if got[0].GitOpsSource != tc.want || got[0].RecordedGitOpsSource != tc.recorded {
				t.Errorf("gitops source: got %q (recorded %q), want %q (recorded %q)",
					got[0].GitOpsSource, got[0].RecordedGitOpsSource, tc.want, tc.recorded)
			}
		})
	}
}

// TestReapReportsTheGitOpsSource: the reaper's report names where the released
// reservation lived.
func TestReapReportsTheGitOpsSource(t *testing.T) {
	for name, tc := range map[string]struct {
		request func(string) reservation.Request
		want    string
	}{
		"collections": {testRequest, reservation.GitOpsSourceCollections},
		"extras":      {extrasRequest, reservation.GitOpsSourceExtras},
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := newReapFixture(t, fixtureOptions{})
			req := tc.request(dir)
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
			if len(reaped) != 1 || reaped[0].GitOpsSource != tc.want {
				t.Errorf("reaped: got %+v, want one with gitops source %q", reaped, tc.want)
			}
		})
	}
}
