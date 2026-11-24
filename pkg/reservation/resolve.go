package reservation

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

const (
	helmDir   = "helm"
	chartFile = "Chart.yaml"
	extrasDir = "extras"
)

// chartFromURL returns the chart an OCI URL serves.
func chartFromURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	if url == "" {
		return ""
	}

	return path.Base(url)
}

// resolveChart returns the chart name a reservation matches on. Reserve,
// Release and List all key their files and their ConfigMap entry on this
// return value, never on whatever a caller typed, so all three call it the
// same way.
//
// app wins whenever it is given. It is both the disambiguator for a repo
// holding several charts and the override for a chart whose name does not
// match the chart its URL serves.
func resolveChart(app, appDir string) (string, error) {
	if app != "" {
		return app, nil
	}
	if appDir == "" {
		return "", microerror.Maskf(invalidConfigError,
			"an app is required: pass App, or AppDir pointing at a checkout of the app repository. The chart name comes from the app repository, never from the repository name")
	}

	return chartFromRepo(appDir)
}

// chartFromRepo reads the chart name out of the app repository's
// helm/*/Chart.yaml. The repository name is not the chart name, and neither is
// the directory name, so the `name` field is the only answer.
func chartFromRepo(dir string) (string, error) {
	helm := filepath.Join(dir, helmDir)

	entries, err := os.ReadDir(helm)
	if err != nil {
		return "", microerror.Maskf(invalidConfigError,
			"cannot read %s to find the chart name: %v. Name the app explicitly to reserve it anyway", helm, err)
	}

	var charts []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		file := filepath.Join(helm, e.Name(), chartFile)
		b, err := os.ReadFile(file) //nolint:gosec // the path is built from the app repository checkout
		if err != nil {
			continue
		}
		var chart struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(b, &chart); err != nil {
			return "", microerror.Maskf(invalidConfigError, "parsing %s: %v", file, err)
		}
		if chart.Name == "" {
			return "", microerror.Maskf(invalidConfigError,
				"%s declares no chart name. Name the app explicitly to reserve it anyway", file)
		}
		charts = append(charts, chart.Name)
	}
	sort.Strings(charts)

	switch len(charts) {
	case 1:
		return charts[0], nil
	case 0:
		return "", microerror.Maskf(invalidConfigError,
			"the app repository at %s holds no %s/*/%s, so there is no chart name to look for. Name the app explicitly to reserve it anyway",
			dir, helmDir, chartFile)
	default:
		return "", microerror.Maskf(appAmbiguousError,
			"the app repository at %s holds %d charts (%s). Name the one to reserve",
			dir, len(charts), strings.Join(charts, ", "))
	}
}

// resolveCollections finds the app in a render of the cluster's collections,
// given the sources collectionSources found there, which match the source
// objects on the chart their URL serves.
//
// It never matches on the object name: 17 of 90 collection names repeat across
// collections, and 5 clusters reference several collections at once, so a name
// match is ambiguous by construction.
func resolveCollections(objects []object, req Request, chart string, sources []object) (blockResolution, error) {
	if inline := inlineChartReleases(objects, chart); len(inline) > 0 {
		return blockResolution{}, microerror.Maskf(appNotSupportedError,
			"%s %s on %s carries chart %q inline under spec.chart instead of referencing it with spec.chartRef. A reservation replaces a chart reference, so this version cannot move it",
			helmReleaseKind, strings.Join(inline, ", "), req.Cluster, chart)
	}

	if len(sources) == 0 {
		return blockResolution{}, microerror.Mask(noMatchError(req, chart))
	}

	res := blockResolution{sources: sources, chartRefs: map[string]string{}}
	sourceNames := res.sourceNames()

	for _, o := range objects {
		if o.kind() != helmReleaseKind || o.namespace() != Namespace {
			continue
		}
		if name, _ := o.nested("spec", "chartRef", "name").(string); slices.Contains(sourceNames, name) {
			res.helmReleases = append(res.helmReleases, Namespace+"/"+o.name())
			res.chartRefs[Namespace+"/"+o.name()] = chartRefOf(o)
		}
	}
	sort.Strings(res.helmReleases)

	if len(res.helmReleases) == 0 {
		return blockResolution{}, microerror.Maskf(appNotFoundError,
			"the rendered collections of %s serve chart %q from %s %s, but no %s takes its chart from any of them, so a reservation would change nothing",
			req.Cluster, chart, ociRepositoryKind, strings.Join(sourceNames, ", "), helmReleaseKind)
	}

	return res, nil
}

// collectionSources returns the sources of the chart in a render of
// the collections, sorted by name.
func collectionSources(objects []object, chart string) []object {
	var sources []object
	for _, o := range objects {
		if o.kind() != ociRepositoryKind || o.namespace() != Namespace {
			continue
		}
		if url, _ := o.nested("spec", "url").(string); chartFromURL(url) == chart {
			sources = append(sources, o)
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].name() < sources[j].name() })

	return sources
}

// inlineChartReleases names the releases that carry the chart inline under
// spec.chart. Collection apps all use spec.chartRef today, so this is the
// refusal for an app shaped in a way the reservation cannot patch.
func inlineChartReleases(objects []object, chart string) []string {
	var names []string
	for _, o := range objects {
		if o.kind() != helmReleaseKind || o.namespace() != Namespace {
			continue
		}
		if name, _ := o.nested("spec", "chart", "spec", "chart").(string); name == chart {
			names = append(names, o.name())
		}
	}
	sort.Strings(names)

	return names
}

// noMatchError explains a chart that neither the collections nor the extras
// of the cluster serve.
func noMatchError(req Request, chart string) error {
	return microerror.Maskf(appNotFoundError,
		"neither a render of %s nor one of %s holds an %s serving chart %q. Name the app explicitly if its chart name differs from the chart its URL serves",
		req.clusterPath(collectionsDir), req.clusterPath(extrasDir), ociRepositoryKind, chart)
}
