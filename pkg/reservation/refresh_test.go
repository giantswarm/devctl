package reservation_test

import (
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"

	"github.com/giantswarm/devctl/v8/internal/gittest"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// A repeated reserve with the same arguments resets the expiry: one entry, one
// component, the original source still the template.
func TestReserveSameArgumentsRefreshesTheExpiry(t *testing.T) {
	for _, scope := range []string{reservation.ScopeApp, reservation.ScopeExclusive} {
		t.Run(scope, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})

			first := testRequest(dir)
			first.Scope = scope
			first.Duration = time.Hour
			if _, err := reservation.Reserve(first); err != nil {
				t.Fatal(err)
			}

			second := first
			second.Now = first.Now.Add(30 * time.Minute)
			second.Duration = 2 * time.Hour
			res, err := reservation.Reserve(second)
			if err != nil {
				t.Fatalf("a repeated reserve was refused: %v", err)
			}
			if want := second.Now.Add(2 * time.Hour); !res.Until.Equal(want) {
				t.Errorf("until: got %s, want %s", res.Until, want)
			}

			got, err := reservation.List(testListRequest(dir))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || !got[0].Until.Equal(res.Until) || !got[0].From.Equal(second.Now) {
				t.Fatalf("want one refreshed reservation, got %+v", got)
			}

			if n := strings.Count(readFixtureFile(t, dir, pathCollections), "reservations/"+fixtureApp); n != 1 {
				t.Errorf("component listed %d times, want 1", n)
			}
			// The dev source is still a copy of the original, not of itself.
			src := readFixtureFile(t, dir, pathSource)
			if !strings.Contains(src, "oci://gsoci.azurecr.io/charts/giantswarm/"+fixtureApp) ||
				!strings.Contains(src, "secretRef") {
				t.Errorf("dev source lost the original source's fields:\n%s", src)
			}
		})
	}
}

func TestReserveRefusesARepeatThatDiffers(t *testing.T) {
	for name, change := range map[string]func(*reservation.Request){
		"branch": func(r *reservation.Request) { r.Branch = testOtherBranch },
		"pr":     func(r *reservation.Request) { r.PullRequest = "giantswarm/hello-world#999" },
	} {
		t.Run(name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})
			first := testRequest(dir)
			if _, err := reservation.Reserve(first); err != nil {
				t.Fatal(err)
			}

			second := first
			change(&second)
			if _, err := reservation.Reserve(second); err == nil || !reservation.IsAlreadyReserved(err) {
				t.Fatalf("expected an already-reserved refusal, got %v", err)
			}
		})
	}
}

func TestCommitAuthorIsTheGitIdentityNotTheUser(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	gittest.RunGit(t, dir, "config", "user.name", "Dev Person")
	gittest.RunGit(t, dir, "config", "user.email", "dev@example.com")

	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}

	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct{ name, email string }{{c.Author.Name, c.Author.Email}, {c.Committer.Name, c.Committer.Email}} {
		if s.name != "Dev Person" || s.email != "dev@example.com" {
			t.Errorf("identity: got %s <%s>, want the git config identity", s.name, s.email)
		}
	}
	if !strings.Contains(c.Message, testUser) {
		t.Errorf("message does not name the holder: %q", c.Message)
	}
}

func TestReserveFailsClearlyWithoutAGitIdentity(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	gittest.RunGit(t, dir, "config", "--unset", "user.name")
	gittest.RunGit(t, dir, "config", "--unset", "user.email")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := reservation.Reserve(testRequest(dir))
	if err == nil || !strings.Contains(err.Error(), "git user.name and user.email are not set for "+dir) {
		t.Fatalf("expected the missing-identity error, got %v", err)
	}
}
