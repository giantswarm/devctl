package reservation_test

import (
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// An older devctl reserved a collections chart with a component under
// collections/reservations/<chart>. This devctl cannot undo that, so it refuses
// to touch the chart and points to the older devctl.

const oldComponent = "management-clusters/" + fixtureCluster + "/collections/reservations/" + fixtureApp + "/kustomization.yaml"

func TestReserveRefusesAChartWithAnOldStyleReservation(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{oldComponent: "kind: Component\n"}})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(testRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, "collections/reservations/"+fixtureApp, "older devctl")
}

func TestReleaseRefusesAChartWithAnOldStyleReservation(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})
	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}
	// The ConfigMap entry stays; the block goes and the old component takes
	// its place, as an older devctl left it.
	writeLocalFixtureFile(t, dir, oldComponent, "kind: Component\n")
	writeAndCommit(t, dir, pathCollections, newGitOpsFixtureFile(t, pathCollections))
	head, _ := headFiles(t, dir)

	_, err := reservation.Release(testReleaseRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, "collections/reservations/"+fixtureApp, "older devctl")
}

// newGitOpsFixtureFile returns a file as a fresh fixture holds it.
func newGitOpsFixtureFile(t *testing.T, name string) string {
	t.Helper()

	return readFixtureFile(t, newGitOpsFixture(t, fixtureOptions{}), name)
}
