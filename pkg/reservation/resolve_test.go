package reservation_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// collectionApp renders one collection app the way an MCB collection ships it:
// an OCIRepository serving a chart, and a HelmRelease taking its chart from it.
// The object name, the chart in the URL and the release name are separate
// arguments, because in the fleet they are not always the same string.
func collectionApp(objectName, chart, releaseName string) string {
	return fmt.Sprintf(`---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: %[1]s
  namespace: giantswarm
spec:
  interval: 10m
  provider: generic
  ref:
    semver: x.x.x
  url: oci://gsoci.azurecr.io/charts/giantswarm/%[2]s
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: %[3]s
  namespace: giantswarm
spec:
  chartRef:
    kind: OCIRepository
    name: %[1]s
    namespace: giantswarm
  interval: 5m
  releaseName: %[3]s
  storageNamespace: giantswarm
  targetNamespace: giantswarm
`, objectName, chart, releaseName)
}

// newAppRepoFixture writes an app repository holding one Chart.yaml per entry,
// keyed by the helm/ subdirectory and valued by the chart name. The two differ
// often enough that the chart name is the only thing worth reading.
func newAppRepoFixture(t *testing.T, charts map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for chartDir, name := range charts {
		path := filepath.Join(dir, "helm", chartDir, "Chart.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a test fixture
			t.Fatal(err)
		}
		content := fmt.Sprintf("apiVersion: v2\nname: %s\nversion: 0.1.0\n", name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // a test fixture
			t.Fatal(err)
		}
	}

	return dir
}

// chartRefName returns the source object a rendered HelmRelease takes its chart
// from.
func chartRefName(t *testing.T, objects map[string]map[string]any, release string) any {
	t.Helper()

	spec, _ := mustObject(t, objects, "HelmRelease/"+release)["spec"].(map[string]any)
	chartRef, _ := spec["chartRef"].(map[string]any)

	return chartRef["name"]
}

// TestReserveMatchesOnTheChartURLNotOnTheObjectName puts a decoy in the render:
// an unrelated app whose OCIRepository is named after our chart. 17 of 90
// collection names repeat across collections, so a name match is ambiguous by
// construction and would move the wrong app here.
func TestReserveMatchesOnTheChartURLNotOnTheObjectName(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		fixtureAppFile: collectionApp("hello-world-app", fixtureApp, "hello-world-app") +
			collectionApp(fixtureApp, "decoy-chart", "decoy"),
	}})

	req := testRequest(dir)
	req.App = ""
	req.AppDir = newAppRepoFixture(t, map[string]string{"hello-world-chart": fixtureApp})

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	if res.App != fixtureApp {
		t.Errorf("resolved app: got %q, want %q", res.App, fixtureApp)
	}

	objects := renderCluster(t, dir, fixtureCluster)
	if got := chartRefName(t, objects, "hello-world-app"); got != res.SourceName {
		t.Errorf("the release serving the chart was not patched: chartRef name %v, want %q", got, res.SourceName)
	}
	if got := chartRefName(t, objects, "decoy"); got != fixtureApp {
		t.Errorf("the decoy named after the chart was patched: chartRef name %v", got)
	}
}

// TestReserveRefusesAnAppTheRenderDoesNotHold keeps the failure loud. A wrong
// lookup looks exactly like an app that does not move, so a quiet one is the
// worst outcome here.
func TestReserveRefusesAnAppTheRenderDoesNotHold(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.App = "nonesuch"

	_, err := reservation.Reserve(req)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsAppNotFound(err) {
		t.Fatalf("expected an app-not-found error, got %v", err)
	}
	for _, want := range []string{"nonesuch", fixtureCluster, "collection"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}

// TestReserveRefusesAnExtrasApp names the real reason. "Not found" would send
// the reader looking for a typo in a command that was right.
func TestReserveRefusesAnExtrasApp(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		"management-clusters/" + fixtureCluster + "/extras/mystery/helmrelease.yaml": collectionApp("mystery", "mystery-app", "mystery"),
	}})

	req := testRequest(dir)
	req.App = "mystery-app"

	_, err := reservation.Reserve(req)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsAppNotSupported(err) {
		t.Fatalf("expected an app-not-supported error, got %v", err)
	}
	for _, want := range []string{"mystery-app", "extras", "collection"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}

// TestReserveRefusesAnInlineChartRelease covers a release that carries its chart
// inline. A reservation replaces a chart reference, so there is nothing to point
// at the dev builds.
func TestReserveRefusesAnInlineChartRelease(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		fixtureAppFile: `---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: ` + fixtureApp + `
  namespace: giantswarm
spec:
  chart:
    spec:
      chart: ` + fixtureApp + `
      sourceRef:
        kind: HelmRepository
        name: giantswarm
  interval: 5m
`,
	}})

	_, err := reservation.Reserve(testRequest(dir))
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsAppNotSupported(err) {
		t.Fatalf("expected an app-not-supported error, got %v", err)
	}
	for _, want := range []string{fixtureApp, "spec.chart", "chartRef"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %s", want, err.Error())
		}
	}
}

// TestReserveNeedsAnAppWhenTheRepoHoldsSeveralCharts asks rather than guesses.
func TestReserveNeedsAnAppWhenTheRepoHoldsSeveralCharts(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.App = ""
	req.AppDir = newAppRepoFixture(t, map[string]string{
		"first":  "alpha",
		"second": "beta",
	})

	_, err := reservation.Reserve(req)
	if err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !reservation.IsAppAmbiguous(err) {
		t.Fatalf("expected an app-ambiguous error, got %v", err)
	}
	// The refusal has to name the candidates, or the reader has to go and read
	// the repository to find out what to retry with.
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name the chart %q: %s", want, err.Error())
		}
	}
}

// TestReserveAppArgumentOverridesTheChartName covers a chart whose name does not
// match the chart its URL serves. The argument wins.
func TestReserveAppArgumentOverridesTheChartName(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{})

	req := testRequest(dir)
	req.App = fixtureOtherApp
	req.AppDir = newAppRepoFixture(t, map[string]string{"chart": fixtureApp})

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.App != fixtureOtherApp {
		t.Errorf("resolved app: got %q, want %q", res.App, fixtureOtherApp)
	}

	objects := renderCluster(t, dir, fixtureCluster)
	if got := chartRefName(t, objects, fixtureOtherApp); got != res.SourceName {
		t.Errorf("other-app chartRef name: got %v, want %q", got, res.SourceName)
	}
	if got := chartRefName(t, objects, fixtureApp); got != fixtureApp {
		t.Errorf("%s was moved by an override naming another app: chartRef name %v", fixtureApp, got)
	}
}

// TestReservePatchesEveryInstanceOfTheApp covers an app running twice on one
// cluster. Both instances share the chart and the branch, so one source object
// serves both.
func TestReservePatchesEveryInstanceOfTheApp(t *testing.T) {
	dir := newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		fixtureAppFile: collectionApp(fixtureApp, fixtureApp, fixtureApp) +
			collectionApp(fixtureApp+"-eu", fixtureApp, fixtureApp+"-eu"),
	}})

	res, err := reservation.Reserve(testRequest(dir))
	if err != nil {
		t.Fatal(err)
	}

	// One source object, not one per instance.
	want := []string{
		"management-clusters/" + fixtureCluster + "/collections/kustomization.yaml",
		"management-clusters/" + fixtureCluster + "/collections/reservations/" + fixtureApp + "/" + fixtureApp + "-dev-reservation.yaml",
		"management-clusters/" + fixtureCluster + "/collections/reservations/" + fixtureApp + "/kustomization.yaml",
		"management-clusters/" + fixtureCluster + "/" + reservation.ConfigMapFile,
	}
	if diff := cmp.Diff(want, res.Files); diff != "" {
		t.Errorf("written files (-want +got):\n%s", diff)
	}

	objects := renderCluster(t, dir, fixtureCluster)
	mustObject(t, objects, "OCIRepository/"+res.SourceName)
	for _, release := range []string{fixtureApp, fixtureApp + "-eu"} {
		if got := chartRefName(t, objects, release); got != res.SourceName {
			t.Errorf("%s chartRef name: got %v, want %q", release, got, res.SourceName)
		}
	}
}
