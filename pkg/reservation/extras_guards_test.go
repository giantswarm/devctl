package reservation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// assertRefused checks that err is a refusal that names every want, and that
// the refused request left HEAD and the working tree alone.
func assertRefused(t *testing.T, dir, head string, err error, is func(error) bool, want ...string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !is(err) {
		t.Fatalf("unexpected error kind: %v", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("refusal does not mention %q: %s", w, err.Error())
		}
	}
	if after, _ := headFiles(t, dir); after != head {
		t.Error("a refused reserve made a commit")
	}
	assertClean(t, dir)
}

// reservedExtras returns the extras kustomization as a reserve of extras-app
// writes it.
func reservedExtras(t *testing.T) string {
	t.Helper()

	dir := newGitOpsFixture(t, fixtureOptions{})
	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}

	return readFixtureFile(t, dir, pathExtras)
}

const humanPatch = `  - patch: |-
      - op: replace
        path: /spec/ref/semver
        value: ">=2.0.0"
    target:
      kind: OCIRepository
      name: extras-app
`

// TestReserveExtrasRefusesAPatchAfterABlock is guard 2: a patch after a block
// can change the source's semver under the dev semverFilter, and then nothing
// matches. The block has to stay last.
func TestReserveExtrasRefusesAPatchAfterABlock(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		pathExtras: reservedExtras(t) + humanPatch,
	}})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(extrasRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, pathExtras, "after")
}

// TestCheckExtrasPatches is guard 2 as the CI check of the GitOps repo uses it.
func TestCheckExtrasPatches(t *testing.T) {
	reserved := reservedExtras(t)
	for name, tc := range map[string]struct {
		content string
		ok      bool
	}{
		"no block":            {readFixtureFile(t, newGitOpsFixture(t, fixtureOptions{}), pathExtras), true},
		"block last":          {reserved, true},
		"patch after a block": {reserved + humanPatch, false},
		// kustomize runs patchesJson6902, replacements and transformers after
		// patches:, so they too come after the blocks.
		"json6902 on a source":      {reserved + json6902("OCIRepository"), false},
		"json6902 without a kind":   {reserved + json6902(""), false},
		"json6902 on a deployment":  {reserved + json6902("Deployment"), true},
		"json6902 without a block":  {readFixtureFile(t, newGitOpsFixture(t, fixtureOptions{}), pathExtras) + json6902("OCIRepository"), true},
		"replacement on a source":   {reserved + replacement("OCIRepository"), false},
		"replacement on a release":  {reserved + replacement("HelmRelease"), true},
		"replacement from a file":   {reserved + "replacements:\n  - path: replacement.yaml\n", false},
		"transformers after blocks": {reserved + "transformers:\n  - transformer.yaml\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "kustomization.yaml")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := reservation.CheckExtrasPatches(path)
			if tc.ok && err != nil {
				t.Errorf("unexpected refusal: %v", err)
			}
			if !tc.ok && !reservation.IsAppNotSupported(err) {
				t.Errorf("expected an app-not-supported error, got %v", err)
			}
		})
	}
}

func json6902(kind string) string {
	return `patchesJson6902:
  - target:
      kind: ` + kind + `
      name: extras-app
    patch: |-
      - op: replace
        path: /spec/interval
        value: 5m
`
}

func replacement(kind string) string {
	return `replacements:
  - source:
      kind: ConfigMap
      name: versions
      fieldPath: data.extras-app
    targets:
      - select:
          kind: ` + kind + `
        fieldPaths:
          - spec.ref.semver
`
}

// TestReserveExtrasRefusesAHiddenChartRef is guard 3: a release outside the
// extras render that takes its chart from the patched source would move too,
// without anybody seeing it in the render.
func TestReserveExtrasRefusesAHiddenChartRef(t *testing.T) {
	hidden := "management-clusters/" + fixtureCluster + "/other/hidden.yaml"
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		hidden: extrasRelease("hidden", "monitoring", fixtureExtrasApp, "flux-giantswarm"),
	}})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(extrasRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, hidden, "monitoring/hidden")
}

// TestReserveExtrasRefusesAHiddenChartRefWithoutANamespace: a release file
// without a namespace gets one from a kustomization or a Flux Kustomization
// that guard 3 does not read, so it matches on the source name alone.
func TestReserveExtrasRefusesAHiddenChartRefWithoutANamespace(t *testing.T) {
	hidden := "management-clusters/" + fixtureCluster + "/other/hidden.yaml"
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		hidden: extrasRelease("hidden", "", fixtureExtrasApp, ""),
	}})
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(extrasRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, hidden, "hidden")
}

// TestReserveExtrasMatchesARenderedReleaseWithoutANamespace: an extras release
// whose file has no namespace, as the extras kustomization sets it, is the
// rendered release, not a hidden one.
func TestReserveExtrasMatchesARenderedReleaseWithoutANamespace(t *testing.T) {
	root := "management-clusters/" + fixtureCluster + "/extras/extras-app/"
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		root + "kustomization.yaml": "namespace: flux-giantswarm\nresources:\n  - extras-app.yaml\n",
		root + "extras-app.yaml": extrasSource(fixtureExtrasApp, "flux-giantswarm", fixtureExtrasApp) +
			extrasRelease(fixtureExtrasApp, "", fixtureExtrasApp, ""),
	}})

	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}
}

// TestReserveExtrasIgnoresASameNameSourceInAnotherNamespace: guard 3 matches on
// name and namespace.
func TestReserveExtrasIgnoresASameNameSourceInAnotherNamespace(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		"management-clusters/" + fixtureCluster + "/other/elsewhere.yaml": extrasRelease("elsewhere", "monitoring", fixtureExtrasApp, ""),
	}})

	if _, err := reservation.Reserve(extrasRequest(dir)); err != nil {
		t.Fatal(err)
	}
}

// TestReserveRefusesAChartInBothTrees is guard 4: the chart is the key of a
// reservation, so it must name one place.
func TestReserveRefusesAChartInBothTrees(t *testing.T) {
	dir := extrasWith(t, extrasSource("hw", "flux-giantswarm", fixtureApp)+
		extrasRelease("hw", "flux-giantswarm", "hw", ""))
	head, _ := headFiles(t, dir)
	req := testRequest(dir)

	_, err := reservation.Reserve(req)
	assertRefused(t, dir, head, err, reservation.IsAppAmbiguous, fixtureApp, "collections", "extras")
}

// TestReserveRefusesAnInlineChartInTheExtras: as for collections, a reservation
// replaces a chart reference, so an inline chart cannot move.
func TestReserveRefusesAnInlineChartInTheExtras(t *testing.T) {
	dir := extrasWith(t, `---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: inline
  namespace: flux-giantswarm
spec:
  chart:
    spec:
      chart: inline-app
  interval: 5m
`)
	head, _ := headFiles(t, dir)
	req := extrasRequest(dir)
	req.App = "inline-app"

	_, err := reservation.Reserve(req)
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, "inline-app", "spec.chart")
}
