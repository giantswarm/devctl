package reservation

import (
	"strings"
	"testing"
)

// TestAssertBlockReservedRefusesAChangedChartRef: a reservation moves a release
// by changing its source, never by changing what the release references.
func TestAssertBlockReservedRefusesAChangedChartRef(t *testing.T) {
	source := object{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       ociRepositoryKind,
		"metadata":   map[string]any{"name": "app", "namespace": "giantswarm"},
		"spec":       map[string]any{"interval": SourceInterval, "ref": devRef("filter")},
	}
	release := func(name string) object {
		return object{
			"kind":     helmReleaseKind,
			"metadata": map[string]any{"name": "app", "namespace": "giantswarm"},
			"spec":     map[string]any{"chartRef": map[string]any{"kind": ociRepositoryKind, "name": name, "namespace": "giantswarm"}},
		}
	}
	resolved := blockResolution{
		sources:      []object{source},
		helmReleases: []string{"giantswarm/app"},
		chartRefs:    map[string]string{"giantswarm/app": chartRefOf(release("app"))},
	}

	if err := assertBlockReserved([]object{source, release("app")}, resolved, "filter", nil); err != nil {
		t.Fatalf("unchanged chartRef: %v", err)
	}
	err := assertBlockReserved([]object{source, release("other")}, resolved, "filter", nil)
	if err == nil || !strings.Contains(err.Error(), "chartRef") || !IsRenderAssertion(err) {
		t.Fatalf("expected a render assertion naming chartRef, got %v", err)
	}
	err = assertBlockReserved([]object{source}, resolved, "filter", nil)
	if !IsRenderAssertion(err) {
		t.Fatalf("expected a render assertion for a lost release, got %v", err)
	}
}
