package reservation_test

import (
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// TestReserveRefusesAppScopedWhenExclusiveIsActive is rule 2: an exclusive
// reservation locks the whole cluster, so an unrelated app-scoped request
// fails against it even though the two never share an app.
func TestReserveRefusesAppScopedWhenExclusiveIsActive(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	exclusive := testRequest(dir)
	exclusive.Scope = reservation.ScopeExclusive
	if _, err := reservation.Reserve(exclusive); err != nil {
		t.Fatal(err)
	}

	appScoped := testRequest(dir)
	appScoped.App = fixtureOtherApp
	appScoped.User = testOtherUser
	appScoped.Branch = testOtherBranch

	_, err := reservation.Reserve(appScoped)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsClusterLocked(err) {
		t.Fatalf("expected a cluster-locked error, got %v", err)
	}
	// The refusal has to name the holder, the app, the branch and the expiry of
	// the exclusive reservation that blocks the request.
	for _, want := range []string{fixtureApp, testUser, testBranch, "2026-09-15T20:00:00Z"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}
