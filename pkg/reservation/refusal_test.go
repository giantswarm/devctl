package reservation_test

import (
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestReserveRefusalMakesNoCommit is rule 4's other half: a refusal changes
// nothing anywhere. Every collision refusal is checked before Reserve writes
// a single file, so HEAD, and the working tree, must be exactly what they
// were before the call.
func TestReserveRefusalMakesNoCommit(t *testing.T) {
	for name, collide := range map[string]func(dir string) reservation.Request{
		"already reserved": func(dir string) reservation.Request {
			second := testRequest(dir)
			second.User = testOtherUser
			second.Branch = testOtherBranch
			return second
		},
		"cluster locked": func(dir string) reservation.Request {
			appScoped := testRequest(dir)
			appScoped.App = fixtureOtherApp
			appScoped.User = testOtherUser
			appScoped.Branch = testOtherBranch
			return appScoped
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := newGitOpsFixture(t, fixtureOptions{})

			first := testRequest(dir)
			if name == "cluster locked" {
				first.Scope = reservation.ScopeExclusive
			}
			if _, err := reservation.Reserve(first); err != nil {
				t.Fatal(err)
			}

			repo, err := git.PlainOpen(dir)
			if err != nil {
				t.Fatal(err)
			}
			before, err := repo.Head()
			if err != nil {
				t.Fatal(err)
			}

			if _, err := reservation.Reserve(collide(dir)); err == nil {
				t.Fatal("expected a refusal, got none")
			}

			after, err := repo.Head()
			if err != nil {
				t.Fatal(err)
			}
			if after.Hash() != before.Hash() {
				t.Errorf("a refusal committed anyway: HEAD moved from %s to %s", before.Hash(), after.Hash())
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
				t.Errorf("a refusal left the working tree dirty:\n%s", status)
			}
		})
	}
}
