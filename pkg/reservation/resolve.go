package reservation

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
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

// urlField matches the value of a `url:` key in a YAML file. It reads the
// extras tree, which is scanned as text rather than rendered.
var urlField = regexp.MustCompile(`(?m)^\s*url:\s*['"]?(\S+?)['"]?\s*$`)

// resolution is the app a reservation moves: the chart it ships, the source
// objects serving that chart, and every release running it.
type resolution struct {
	// chart is the chart name. It keys the reservation and names its files.
	chart string
	// original is the source object the reservation's own object is copied from.
	original object
	// sourceNames names every matched OCIRepository, sorted. All of them have to
	// come out of the render with their version selector untouched.
	sourceNames []string
	// originalRefs holds the `spec.ref` each matched source carried before the
	// reservation, keyed by object name.
	originalRefs map[string]any
	// helmReleases names every release to patch, sorted. An app can run more
	// than once on one cluster.
	helmReleases []string
}

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

// resolveApp finds the app in a render of the cluster's collections, matching
// the source objects on the chart their URL serves.
//
// It never matches on the object name: 17 of 90 collection names repeat across
// collections, and 5 clusters reference several collections at once, so a name
// match is ambiguous by construction.
func resolveApp(objects []object, req Request, chart string) (resolution, error) {
	if inline := inlineChartReleases(objects, chart); len(inline) > 0 {
		return resolution{}, microerror.Maskf(appNotSupportedError,
			"%s %s on %s carries chart %q inline under spec.chart instead of referencing it with spec.chartRef. A reservation replaces a chart reference, so this version cannot move it",
			helmReleaseKind, strings.Join(inline, ", "), req.Cluster, chart)
	}

	res := resolution{chart: chart, originalRefs: map[string]any{}}

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

	if len(sources) == 0 {
		return resolution{}, microerror.Mask(noMatchError(req, chart))
	}

	// One chart, one branch, so one source object however many instances run.
	// The first by name is the template; instances of one app share a chart URL,
	// and everything else the copy keeps is a property of that chart.
	res.original = sources[0]
	for _, s := range sources {
		res.sourceNames = append(res.sourceNames, s.name())
		res.originalRefs[s.name()] = s.nested("spec", "ref")
	}

	for _, o := range objects {
		if o.kind() != helmReleaseKind || o.namespace() != Namespace {
			continue
		}
		if name, _ := o.nested("spec", "chartRef", "name").(string); slices.Contains(res.sourceNames, name) {
			res.helmReleases = append(res.helmReleases, o.name())
		}
	}
	sort.Strings(res.helmReleases)

	if len(res.helmReleases) == 0 {
		return resolution{}, microerror.Maskf(appNotFoundError,
			"the rendered collections of %s serve chart %q from %s %s, but no %s takes its chart from any of them, so a reservation would change nothing",
			req.Cluster, chart, ociRepositoryKind, strings.Join(res.sourceNames, ", "), helmReleaseKind)
	}

	return res, nil
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

// noMatchError explains a render that holds no such chart. It looks in the
// cluster's extras tree first: "not found" would send the reader hunting for a
// typo in a command that was right.
func noMatchError(req Request, chart string) error {
	if files := extrasFilesServing(req, chart); len(files) > 0 {
		return microerror.Maskf(appNotSupportedError,
			"chart %q is served from the extras folder of %s (%s), not from its collections. This version covers collection apps only",
			chart, req.Cluster, strings.Join(files, ", "))
	}

	return microerror.Maskf(appNotFoundError,
		"a render of %s holds no %s in namespace %s serving chart %q, so %q is not a collection app on %s. Name the app explicitly if its chart name differs from the chart its URL serves",
		req.clusterPath(collectionsDir), ociRepositoryKind, Namespace, chart, chart, req.Cluster)
}

// extrasFilesServing returns the repo-relative extras files that mention a URL
// serving the chart.
//
// It is a text scan, not a render: extras kustomizations nest two levels deep
// and are wired up by Flux Kustomization objects, so there is no single overlay
// to build. It only has to be good enough to turn a bare "not found" into the
// real reason.
// ponytail: text scan over extras; render it properly only if extras ever
// becomes supported.
func extrasFilesServing(req Request, chart string) []string {
	root := filepath.Join(req.RepoDir, clustersDir, req.Cluster, extrasDir)

	var files []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // a missing or unreadable extras tree just means no hint
		}
		if ext := filepath.Ext(p); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // the path comes from walking the repo checkout
		if err != nil {
			return nil //nolint:nilerr // an unreadable file just means no hint
		}
		for _, m := range urlField.FindAllStringSubmatch(string(b), -1) {
			if chartFromURL(m[1]) == chart {
				rel, err := filepath.Rel(req.RepoDir, p)
				if err != nil {
					rel = p
				}
				files = append(files, filepath.ToSlash(rel))
				break
			}
		}

		return nil
	})
	sort.Strings(files)

	return files
}
