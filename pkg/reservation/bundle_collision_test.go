package reservation_test

import (
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// reserveThenRefuse reserves first, then expects the refusal of second
// with no change to the repo.
func reserveThenRefuse(t *testing.T, first, second func(dir string) reservation.Request, is func(error) bool, want ...string) {
	t.Helper()

	dir := bundleFixture(t, reservation.GitOpsSourceCollections, "")
	if _, err := reservation.Reserve(first(dir)); err != nil {
		t.Fatalf("the first reservation: %v", err)
	}
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(second(dir))
	assertRefused(t, dir, head, err, is, want...)
}

func TestBundleCollisions(t *testing.T) {
	plainBundle := func(dir string) reservation.Request {
		req := bundleRequest(dir)
		req.App, req.Bundle, req.PullRequest = fixtureBundle, "", "giantswarm/observability-bundle#9"
		req.User = testOtherUser
		return req
	}
	member := func(dir string) reservation.Request { return bundleRequest(dir) }

	t.Run("bundle reserved, member refused", func(t *testing.T) {
		reserveThenRefuse(t, plainBundle, member, reservation.IsAlreadyReserved, fixtureBundle, testOtherUser, "bundle")
	})
	t.Run("member reserved, bundle refused", func(t *testing.T) {
		reserveThenRefuse(t, member, plainBundle, reservation.IsAlreadyReserved, fixtureMemberChart, testUser, "member")
	})
	t.Run("different bundle for a reserved chart", func(t *testing.T) {
		other := func(dir string) reservation.Request {
			req := member(dir)
			req.Bundle = "security-bundle"
			return req
		}
		reserveThenRefuse(t, member, other, reservation.IsAlreadyReserved, fixtureMemberChart, fixtureBundle, "security-bundle")
	})
	t.Run("a member reserved as a plain chart", func(t *testing.T) {
		plain := func(dir string) reservation.Request {
			req := member(dir)
			req.Bundle = ""
			return req
		}
		reserveThenRefuse(t, member, plain, reservation.IsAlreadyReserved, fixtureMemberChart, fixtureBundle)
	})
	t.Run("exclusive locks the members too", func(t *testing.T) {
		exclusive := func(dir string) reservation.Request {
			req := testRequest(dir)
			req.User, req.Scope = testOtherUser, reservation.ScopeExclusive
			return req
		}
		reserveThenRefuse(t, member, exclusive, reservation.IsClusterLocked, fixtureMemberChart)
		reserveThenRefuse(t, exclusive, member, reservation.IsClusterLocked, fixtureApp)
	})
}
