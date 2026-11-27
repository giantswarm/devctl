package reservation_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const pathSharedApp = "management-clusters/" + fixtureCluster + "/extras/shared/shared.yaml"

// extrasWith adds a folder "shared" to the extras, holding manifests.
func extrasWith(t *testing.T, manifests string) string {
	t.Helper()

	return newGitOpsFixture(t, fixtureOptions{extraFiles: map[string]string{
		pathExtras: `resources:
  - ./extras-app/
  - ./shared/
`,
		"management-clusters/" + fixtureCluster + "/extras/shared/kustomization.yaml": `resources:
  - shared.yaml
`,
		pathSharedApp: manifests,
	}})
}

func extrasSource(name, namespace, chart string) string {
	return fmt.Sprintf(`---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: %s
  namespace: %s
spec:
  interval: 10m
  ref:
    tag: 1.2.3
  secretRef:
    name: container-registries-configuration
  url: oci://gsoci.azurecr.io/charts/giantswarm/%s
`, name, namespace, chart)
}

func extrasRelease(name, namespace, source, sourceNamespace string) string {
	ref := ""
	if sourceNamespace != "" {
		ref = "\n    namespace: " + sourceNamespace
	}

	return fmt.Sprintf(`---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: %s
  namespace: %s
spec:
  chartRef:
    kind: OCIRepository
    name: %s%s
  interval: 5m
`, name, namespace, source, ref)
}

// TestReserveExtrasMovesEveryReleaseOfASharedSource: one source feeds several
// releases through chartRef, also from another namespace. One patch moves them
// all, and the result names every one, so the bot comment can.
func TestReserveExtrasMovesEveryReleaseOfASharedSource(t *testing.T) {
	dir := extrasWith(t, extrasSource("agent", "flux-giantswarm", "agent")+
		extrasRelease("agent-a", "flux-giantswarm", "agent", "")+
		extrasRelease("agent-b", "monitoring", "agent", "flux-giantswarm")+
		extrasRelease("not-agent", "monitoring", "agent", ""))
	req := extrasRequest(dir)
	req.App = "agent"

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"flux-giantswarm/agent-a", "monitoring/agent-b"}
	if diff := cmp.Diff(want, res.HelmReleases); diff != "" {
		t.Errorf("moved releases (-want +got):\n%s", diff)
	}
	if res.SourceName != "agent" {
		t.Errorf("source name: got %q, want %q", res.SourceName, "agent")
	}
}

// TestReserveExtrasPatchesEverySourceOfAChart: a chart with three sources gets
// all three on the same dev ref, and release takes all three back.
func TestReserveExtrasPatchesEverySourceOfAChart(t *testing.T) {
	var manifests string
	for _, name := range []string{"valkey-a", "valkey-b", "valkey-c"} {
		manifests += extrasSource(name, "flux-giantswarm", "valkey") +
			extrasRelease(name, "flux-giantswarm", name, "")
	}
	dir := extrasWith(t, manifests)
	before := readFixtureFile(t, dir, pathExtras)
	original := renderExtras(t, dir, fixtureCluster)
	req := extrasRequest(dir)
	req.App = "valkey"

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}

	if want := "valkey-a, valkey-b, valkey-c"; res.SourceName != want {
		t.Errorf("source name: got %q, want %q", res.SourceName, want)
	}
	want := []string{"flux-giantswarm/valkey-a", "flux-giantswarm/valkey-b", "flux-giantswarm/valkey-c"}
	if diff := cmp.Diff(want, res.HelmReleases); diff != "" {
		t.Errorf("moved releases (-want +got):\n%s", diff)
	}
	after := renderExtras(t, dir, fixtureCluster)
	for _, name := range []string{"valkey-a", "valkey-b", "valkey-c"} {
		got := mustObject(t, after, "OCIRepository/"+name)
		ref := map[string]any{"semver": ">=0.0.0-0", "semverFilter": res.SemverFilter}
		if diff := cmp.Diff(ref, got["spec"].(map[string]any)["ref"]); diff != "" {
			t.Errorf("%s ref (-want +got):\n%s", name, diff)
		}
		wantSecret := mustObject(t, original, "OCIRepository/"+name)["spec"].(map[string]any)["secretRef"]
		if diff := cmp.Diff(wantSecret, got["spec"].(map[string]any)["secretRef"]); diff != "" {
			t.Errorf("%s secretRef (-want +got):\n%s", name, diff)
		}
	}

	rel := extrasReleaseRequest(dir)
	rel.App = "valkey"
	if _, err := reservation.Release(rel); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(before, readFixtureFile(t, dir, pathExtras)); diff != "" {
		t.Errorf("extras kustomization after release (-before +after):\n%s", diff)
	}
	if diff := cmp.Diff(original, renderExtras(t, dir, fixtureCluster)); diff != "" {
		t.Errorf("render after release (-before +after):\n%s", diff)
	}
}

func TestReserveReportsTheSourceURL(t *testing.T) {
	const url = "oci://gsoci.azurecr.io/charts/giantswarm/agent"

	res, err := reservation.Reserve(func() reservation.Request {
		req := extrasRequest(extrasWith(t, extrasSource("agent", "flux-giantswarm", "agent")+
			extrasRelease("agent", "flux-giantswarm", "agent", "")))
		req.App = "agent"
		return req
	}())
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceURL != url {
		t.Errorf("extras source URL: got %q, want %q", res.SourceURL, url)
	}
}

// TestReserveLeavesTheSourceURLEmptyWhenSourcesDiffer: one URL is all the
// result can hold, so devctl does not pick one of two.
func TestReserveLeavesTheSourceURLEmptyWhenSourcesDiffer(t *testing.T) {
	other := strings.Replace(extrasSource("agent-b", "flux-giantswarm", "agent"),
		"gsoci.azurecr.io/charts/giantswarm", "example.com/charts", 1)
	dir := extrasWith(t, extrasSource("agent-a", "flux-giantswarm", "agent")+other+
		extrasRelease("agent-a", "flux-giantswarm", "agent-a", "")+
		extrasRelease("agent-b", "flux-giantswarm", "agent-b", ""))
	req := extrasRequest(dir)
	req.App = "agent"

	res, err := reservation.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceURL != "" {
		t.Errorf("source URL: got %q, want none for differing URLs", res.SourceURL)
	}
}
