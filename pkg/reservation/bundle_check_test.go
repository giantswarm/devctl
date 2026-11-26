package reservation_test

import (
	"fmt"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

// standInRegistry is the bundle chart pull of the fixture: charts by OCI
// reference, with no network. It has the tags a registry would list.
type standInRegistry map[string]map[string]*chart.Chart

func (s standInRegistry) Tags(ref string) ([]string, error) {
	var tags []string
	for tag := range s[ref] {
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("stand-in registry: no tags for %s", ref)
	}

	return tags, nil
}

func (s standInRegistry) Pull(ref, tag string) (*chart.Chart, error) {
	c, ok := s[ref][tag]
	if !ok {
		return nil, fmt.Errorf("stand-in registry: no %s:%s", ref, tag)
	}

	return c, nil
}

const bundleRef = "gsoci.azurecr.io/charts/giantswarm/" + fixtureBundle

// bundleValues has two members on the chart alloy and one on loki.
const bundleValues = `apps:
  alloyLogs:
    chartName: alloy
    version: 1.0.0
  alloyMetrics:
    chartName: alloy
    version: 1.0.0
  loki:
    chartName: loki
    version: 2.0.0
`

// flux-style: one OCIRepository and one HelmRelease per member.
const bundleTemplate = `{{- range $key, $app := .Values.apps }}
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: {{ $key }}
  namespace: giantswarm
  labels:
    LABEL: {{ $app.chartName }}
spec:
  interval: 10m
  ref:
    tag: {{ $app.version }}
  url: oci://gsoci.azurecr.io/charts/giantswarm/{{ $app.chartName }}
{{- end }}
`

// appCRTemplate is the old shape: one App CR per member.
const appCRTemplate = `{{- range $key, $app := .Values.apps }}
---
apiVersion: application.giantswarm.io/v1alpha1
kind: App
metadata:
  name: {{ $key }}
  namespace: giantswarm
spec:
  name: {{ $app.chartName }}
  version: {{ $app.version }}
{{- end }}
`

// bundleChart panics on a bad chart, as it builds a constant. It builds a bundle chart of the stand-in registry from its values
// and its one template.
func bundleChart(values, template string) *chart.Chart {
	c, err := loader.LoadFiles([]*archive.BufferedFile{
		{Name: "Chart.yaml", Data: []byte("apiVersion: v2\nname: " + fixtureBundle + "\nversion: 1.2.3\n")},
		{Name: "values.yaml", Data: []byte(values)},
		{Name: "templates/members.yaml", Data: []byte(template)},
	})
	if err != nil {
		panic(err)
	}

	return c
}

// labelledBundle is a bundle whose member sources carry the member label.
func labelledBundle() *chart.Chart {
	return bundleChart(bundleValues, strings.ReplaceAll(bundleTemplate, "LABEL", "bundle.giantswarm.io/member-chart"))
}

// bundleAt is a registry that serves c as the one version the fixture's bundle
// source asks for (the tag 1.2.3).
func bundleAt(c *chart.Chart) standInRegistry {
	return standInRegistry{bundleRef: {"1.2.3": c}}
}

func TestReserveBundleRefusesABundleThatDoesNotQualify(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bundle   func() *chart.Chart
		is       func(error) bool
		mentions []string
	}{
		{"app CRs", func() *chart.Chart { return bundleChart(bundleValues, appCRTemplate) },
			reservation.IsAppNotSupported, []string{fixtureBundle, "App", "Flux"}},
		{"chart not a member", func() *chart.Chart {
			return bundleChart("apps:\n  loki:\n    chartName: loki\n    version: 2.0.0\n", strings.ReplaceAll(bundleTemplate, "LABEL", "bundle.giantswarm.io/member-chart"))
		}, reservation.IsAppNotFound, []string{fixtureBundle, fixtureMemberChart, "apps.<key>.chartName", "loki"}},
		{"label missing", func() *chart.Chart {
			return bundleChart(bundleValues, strings.ReplaceAll(bundleTemplate, "LABEL", "some.other/label"))
		}, reservation.IsAppNotSupported, []string{fixtureBundle, "bundle.giantswarm.io/member-chart", "OCIRepository"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := bundleFixture(t, reservation.GitOpsSourceCollections, "")
			head, _ := headFiles(t, dir)
			req := bundleRequest(dir)
			req.BundleRegistry = bundleAt(tc.bundle())

			_, err := reservation.Reserve(req)
			assertRefused(t, dir, head, err, tc.is, tc.mentions...)
		})
	}
}

// TestReserveBundleChecksTheNewestVersionTheRefAllows: with a semver ref, the
// check reads the highest version of the range, not the first or the last one
// the registry lists, and not one outside it.
func TestReserveBundleChecksTheNewestVersionTheRefAllows(t *testing.T) {
	unlabelled := bundleChart(bundleValues, strings.ReplaceAll(bundleTemplate, "LABEL", "some.other/label"))
	for _, tc := range []struct {
		name    string
		tags    map[string]*chart.Chart
		refused string
	}{
		{"newest in range is labelled", map[string]*chart.Chart{"1.2.3": unlabelled, "1.10.0": labelledBundle(), "2.0.0": unlabelled}, ""},
		{"newest in range is not", map[string]*chart.Chart{"1.2.3": labelledBundle(), "1.10.0": unlabelled, "2.0.0": labelledBundle()}, "1.10.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := bundleOptions(reservation.GitOpsSourceCollections, "")
			const path = "bases/collections/demo/bundle.yaml"
			opts.extraFiles[path] = strings.Replace(opts.extraFiles[path], "tag: 1.2.3", `semver: ">=1.0.0 <2.0.0"`, 1)
			dir := newGitOpsFixture(t, opts)
			head, _ := headFiles(t, dir)
			req := bundleRequest(dir)
			req.BundleRegistry = standInRegistry{bundleRef: tc.tags}

			_, err := reservation.Reserve(req)
			if tc.refused == "" {
				if err != nil {
					t.Fatalf("Reserve: %v", err)
				}
				return
			}
			assertRefused(t, dir, head, err, reservation.IsAppNotSupported, tc.refused)
		})
	}
}

// laterPostRenderer is a kustomization whose patchesJson6902, which kustomize
// runs after patches:, appends a post-renderer to the bundle's HelmRelease:
// after the reservation block.
const laterPostRenderer = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../../bases/collections/demo
patchesJson6902:
  - target:
      kind: HelmRelease
      name: ` + fixtureBundle + `
    patch: |-
      - op: add
        path: /spec/postRenderers/-
        value:
          kustomize:
            patches:
              - target:
                  kind: Deployment
                patch: |-
                  - op: add
                    path: /metadata/labels/late
                    value: "true"
`

func TestReserveBundleRefusesAPostRendererAfterTheReservationOne(t *testing.T) {
	opts := bundleOptions(reservation.GitOpsSourceCollections, existingPostRenderer)
	opts.extraFiles[pathCollections] = laterPostRenderer
	dir := newGitOpsFixture(t, opts)
	head, _ := headFiles(t, dir)

	_, err := reservation.Reserve(bundleRequest(dir))
	assertRefused(t, dir, head, err, reservation.IsAppNotSupported, fixtureBundle, "after a reservation post-renderer", "patchesJson6902")
}
