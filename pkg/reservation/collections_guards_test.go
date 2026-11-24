package reservation_test

import (
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// The collections get the same guards as the extras: the block must stay last,
// nothing may run after it, and no release may move out of sight.

func reservedCollections(t *testing.T) string {
	t.Helper()

	dir := newGitOpsFixture(t, fixtureOptions{})
	if _, err := reservation.Reserve(testRequest(dir)); err != nil {
		t.Fatal(err)
	}

	return readFixtureFile(t, dir, pathCollections)
}

func TestReserveCollectionsRefusesAPatchAfterABlock(t *testing.T) {
	humanPatch := `  - patch: |-
      - op: replace
        path: /spec/ref/semver
        value: ">=2.0.0"
    target:
      kind: OCIRepository
      name: hello-world
`
	dir := newGitOpsFixture(t, fixtureOptions{collectionsKustomization: reservedCollections(t) + humanPatch})
	head, _ := headFiles(t, dir)
	req := testRequest(dir)
	req.App = fixtureOtherApp

	_, err := reservation.Reserve(req)
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, pathCollections, "after")
}

func TestReserveCollectionsRefusesALaterTransform(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{collectionsKustomization: `resources:
  - ../../../bases/collections/demo
patchesJson6902:
  - target:
      kind: OCIRepository
      name: hello-world
    patch: |-
      - op: replace
        path: /spec/interval
        value: 5m
`})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(testRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, pathCollections, "patchesJson6902")
}

func TestReserveCollectionsRefusesAHiddenChartRef(t *testing.T) {
	hidden := "management-clusters/" + fixtureCluster + "/other/hidden.yaml"
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		hidden: extrasRelease("hidden", "monitoring", fixtureApp, "giantswarm"),
	}})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(testRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, hidden, "monitoring/hidden")
}
