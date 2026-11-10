package reservation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

func quoteReservationKey(t *testing.T, dir, app, key string) {
	t.Helper()
	content := readFixtureFile(t, dir, pathReservations)
	old := "  " + app + ":"
	if strings.Count(content, old) != 1 {
		t.Fatalf("expected exactly one entry for %s", app)
	}
	writeLocalFixtureFile(t, dir, pathReservations, strings.Replace(content, old, "  "+key+":", 1))
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(pathReservations); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("quote "+app, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Unix(0, 0).UTC()},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReserveQuotedKey(t *testing.T) {
	for name, key := range map[string]string{
		"single":  "'" + fixtureApp + "'",
		"double":  `"` + fixtureApp + `"`,
		"escaped": `"\x68` + fixtureApp[1:] + `"`,
	} {
		for _, takeover := range []bool{false, true} {
			mode := "refresh"
			if takeover {
				mode = "takeover"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				dir := newGitOpsFixture(t, fixtureOptions{})
				first := testRequest(dir)
				first.Duration = time.Hour
				if _, err := reservation.Reserve(first); err != nil {
					t.Fatal(err)
				}
				other := first
				other.App = fixtureOtherApp
				if _, err := reservation.Reserve(other); err != nil {
					t.Fatal(err)
				}
				quoteReservationKey(t, dir, fixtureApp, key)
				quoteReservationKey(t, dir, fixtureOtherApp, "'"+fixtureOtherApp+"'")
				before, err := reservation.List(testListRequest(dir))
				if err != nil || len(before) != 2 {
					t.Fatalf("initial quoted state: %+v, %v", before, err)
				}
				unrelated := reservationEntries(t, dir, fixtureCluster)[fixtureOtherApp]
				var unrelatedLine string
				for _, line := range strings.Split(readFixtureFile(t, dir, pathReservations), "\n") {
					if strings.HasPrefix(line, "  '"+fixtureOtherApp+"':") {
						unrelatedLine = line
					}
				}

				second := first
				second.Now = first.Now.Add(30 * time.Minute)
				second.Duration = 2 * time.Hour
				if takeover {
					second.Now = first.Now.Add(2 * time.Hour)
					second.User = testOtherUser
					second.Branch = testOtherBranch
					second.PullRequest = "giantswarm/hello-world#456"
				}
				result, err := reservation.Reserve(second)
				if err != nil {
					t.Fatal(err)
				}
				got, err := reservation.List(testListRequest(dir))
				if err != nil || len(got) != 2 {
					t.Fatalf("updated state: %+v, %v", got, err)
				}
				for _, entry := range got {
					if entry.App == fixtureApp && (entry.User != second.User || entry.Branch != second.Branch || entry.PullRequest != second.PullRequest || !entry.From.Equal(second.Now) || !entry.Until.Equal(result.Until)) {
						t.Errorf("updated reservation: %+v", entry)
					}
				}
				if diff := cmp.Diff(unrelated, reservationEntries(t, dir, fixtureCluster)[fixtureOtherApp]); diff != "" {
					t.Errorf("unrelated reservation changed (-want +got):\n%s", diff)
				}
				if unrelatedLine == "" || !strings.Contains(readFixtureFile(t, dir, pathReservations), unrelatedLine+"\n") {
					t.Error("unrelated reservation formatting changed")
				}
				source := mustObject(t, renderCluster(t, dir, fixtureCluster), "OCIRepository/"+result.SourceName)
				annotations := source["metadata"].(map[string]any)["annotations"].(map[string]any)
				if got := annotations[reservation.AnnotationPrefix+"until"]; got != result.Until.Format(time.RFC3339) {
					t.Errorf("source expiry: %v, want %s", got, result.Until)
				}

				// Quote the updated key again to exercise release's lookup and
				// removal independently of the writer's canonical bare spelling.
				quoteReservationKey(t, dir, fixtureApp, key)
				if _, err := reservation.Release(reservation.ReleaseRequest{
					RepoDir: dir, Cluster: fixtureCluster, App: fixtureApp,
					User: second.User, PullRequest: second.PullRequest,
				}); err != nil {
					t.Fatal(err)
				}
				remaining, err := reservation.List(testListRequest(dir))
				if err != nil || len(remaining) != 1 || remaining[0].App != fixtureOtherApp {
					t.Fatalf("release left %+v, %v", remaining, err)
				}
			})
		}
	}
}
