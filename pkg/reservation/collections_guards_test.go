package reservation_test

import (
	"os"
	"path/filepath"
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
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, hidden, "monitoring/hidden", "the collections render")
}

// TestCheckBlockPatchesOnACollectionsReservation: the CI check passes on the
// collections file a reserve wrote, and fails on a human patch after the block.
func TestCheckBlockPatchesOnACollectionsReservation(t *testing.T) {
	reserved := reservedCollections(t)
	for name, tc := range map[string]struct {
		content string
		ok      bool
	}{
		"block last":          {reserved, true},
		"patch after a block": {reserved + "  - path: later.yaml\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kustomization.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := reservation.CheckBlockPatches(path)
			if tc.ok && err != nil {
				t.Errorf("unexpected refusal: %v", err)
			}
			if !tc.ok && !reservation.IsAppNotSupported(err) {
				t.Errorf("expected an app-not-supported error, got %v", err)
			}
		})
	}
}
