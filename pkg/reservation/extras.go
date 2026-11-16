package reservation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
)

// An extras reservation is the reverse of a collection one: it adds no object,
// as flux-extras never prunes, and the bot cannot delete what it added. It
// patches the app's own sources in place, from one marked block at the end of
// the root patches: list of the cluster's extras. Server-side apply drops the
// patched values again when the block goes away.

const (
	patchesKey = "patches:"

	blockBeginMarker = "# BEGIN reservation of %s, written by `devctl reservation reserve`. Do not edit."
	blockEndMarker   = "# END reservation of %s, written by `devctl reservation reserve`"
)

// extrasResolution is an extras app: every source serving its chart, and every
// release taking its chart from one of them.
type extrasResolution struct {
	// sources are the matched OCIRepository objects, sorted by namespace and name.
	sources []object
	// helmReleases are "<namespace>/<name>" of each moved release, sorted.
	helmReleases []string
}

func (r extrasResolution) sourceNames() []string {
	names := make([]string, 0, len(r.sources))
	for _, s := range r.sources {
		names = append(names, s.name())
	}

	return names
}

func extrasKustomizationPath(repoDir, cluster string) string {
	return filepath.Join(repoDir, clustersDir, cluster, extrasDir, "kustomization.yaml")
}

// resolveExtras finds the chart in a render of the cluster's extras. It reports
// false when the cluster has no extras or the extras serve no such chart.
func resolveExtras(req Request, chart string) (extrasResolution, bool, error) {
	path := extrasKustomizationPath(req.RepoDir, req.Cluster)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return extrasResolution{}, false, nil
	}
	objects, err := renderDir(filepath.Dir(path))
	if err != nil {
		return extrasResolution{}, false, microerror.Mask(err)
	}

	// As for collections, a reservation replaces a chart reference, so an
	// inline chart cannot move.
	var inline []string
	for _, o := range objects {
		if o.kind() != helmReleaseKind {
			continue
		}
		if name, _ := o.nested("spec", "chart", "spec", "chart").(string); name == chart {
			inline = append(inline, o.namespace()+"/"+o.name())
		}
	}
	if len(inline) > 0 {
		sort.Strings(inline)
		return extrasResolution{}, false, microerror.Maskf(appNotSupportedError,
			"%s %s in the extras of %s carries chart %q inline under spec.chart instead of referencing it with spec.chartRef. A reservation replaces a chart reference, so it cannot move it",
			helmReleaseKind, strings.Join(inline, ", "), req.Cluster, chart)
	}

	var res extrasResolution
	served := map[string]bool{}
	for _, o := range objects {
		if o.kind() != ociRepositoryKind {
			continue
		}
		if url, _ := o.nested("spec", "url").(string); chartFromURL(url) == chart {
			res.sources = append(res.sources, o)
			served[o.namespace()+"/"+o.name()] = true
		}
	}
	if len(res.sources) == 0 {
		return extrasResolution{}, false, nil
	}
	sort.Slice(res.sources, func(i, j int) bool {
		a, b := res.sources[i], res.sources[j]
		return a.namespace()+"/"+a.name() < b.namespace()+"/"+b.name()
	})

	for _, o := range objects {
		if o.kind() != helmReleaseKind {
			continue
		}
		name, _ := o.nested("spec", "chartRef", "name").(string)
		namespace, _ := o.nested("spec", "chartRef", "namespace").(string)
		if namespace == "" {
			namespace = o.namespace()
		}
		if served[namespace+"/"+name] {
			res.helmReleases = append(res.helmReleases, o.namespace()+"/"+o.name())
		}
	}
	sort.Strings(res.helmReleases)

	if len(res.helmReleases) == 0 {
		return extrasResolution{}, false, microerror.Maskf(appNotFoundError,
			"the rendered extras of %s serve chart %q from %s %s, but no %s takes its chart from any of them, so a reservation would change nothing",
			req.Cluster, chart, ociRepositoryKind, strings.Join(res.sourceNames(), ", "), helmReleaseKind)
	}

	return res, true, nil
}

// reserveExtras writes the reservation block and the ConfigMap entry, checks
// the render, and commits both.
func reserveExtras(req Request, chart string, resolved extrasResolution, from, until time.Time, replaced *Reservation) (res Result, err error) {
	path := extrasKustomizationPath(req.RepoDir, req.Cluster)
	configMapPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, ConfigMapFile)
	semverFilter := devSemverFilter(req.Branch)
	annotations := reservationAnnotations(req, from, until)

	defer func() {
		if err != nil {
			discardExtrasWrites(req.RepoDir, req.Cluster)
		}
	}()

	block, err := extrasBlock(chart, resolved.sources, semverFilter, annotations)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := writeBlock(path, chart, block); err != nil {
		return Result{}, microerror.Mask(err)
	}
	entry, err := reservationEntry(chart, req, from, until)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := writeReservationEntry(configMapPath, chart, entry); err != nil {
		return Result{}, microerror.Mask(err)
	}

	after, err := renderDir(filepath.Dir(path))
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := assertExtrasReserved(after, resolved, semverFilter, annotations); err != nil {
		return Result{}, microerror.Mask(err)
	}

	files := []string{req.clusterPath(ConfigMapFile), req.clusterPath(extrasDir, "kustomization.yaml")}
	sort.Strings(files)

	commit, err := commitAll(req.RepoDir, fmt.Sprintf(
		"reserve %s on %s for %s (branch %s, until %s)",
		chart, req.Cluster, req.User, req.Branch, until.Format(time.RFC3339)), files)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	return Result{
		App:          chart,
		PullRequest:  req.PullRequest,
		Commit:       commit,
		SourceName:   strings.Join(resolved.sourceNames(), ", "),
		HelmReleases: resolved.helmReleases,
		SemverFilter: semverFilter,
		Files:        files,
		From:         from,
		Until:        until,
		Replaced:     replaced,
	}, nil
}

// releaseExtras removes the chart's reservation block and its ConfigMap entry,
// checks the render, and commits both.
func releaseExtras(req ReleaseRequest, chart string) (res ReleaseResult, err error) {
	path := extrasKustomizationPath(req.RepoDir, req.Cluster)
	configMapPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, ConfigMapFile)

	defer func() {
		if err != nil {
			discardExtrasWrites(req.RepoDir, req.Cluster)
		}
	}()

	if err := removeBlock(path, chart); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := removeReservationEntry(configMapPath, chart); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	after, err := renderDir(filepath.Dir(path))
	if err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	for _, o := range after {
		if o.kind() != ociRepositoryKind {
			continue
		}
		url, _ := o.nested("spec", "url").(string)
		if _, ok := o.nested("metadata", "annotations", AnnotationPrefix+annotationUser).(string); ok && chartFromURL(url) == chart {
			return ReleaseResult{}, microerror.Maskf(renderAssertionError,
				"the rendered extras still carry the reservation on %s %s/%s after release", ociRepositoryKind, o.namespace(), o.name())
		}
	}

	files := []string{clusterPath(req.Cluster, ConfigMapFile), clusterPath(req.Cluster, extrasDir, "kustomization.yaml")}
	sort.Strings(files)

	commit, err := commitAll(req.RepoDir, fmt.Sprintf("release %s on %s (by %s)", chart, req.Cluster, req.User), files)
	if err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	return ReleaseResult{App: chart, Commit: commit, Files: files}, nil
}

// discardExtrasWrites restores the two files an extras reservation edits.
func discardExtrasWrites(dir, cluster string) {
	_ = runGit(context.Background(), dir, "checkout", "HEAD", "--",
		clusterPath(cluster, ConfigMapFile), clusterPath(cluster, extrasDir, "kustomization.yaml"))
}

// extrasBlock renders the reservation block as list items at column 0, enclosed
// in its markers. Per source, a JSON patch replaces spec.ref as a whole, so a
// stable tag or semver cannot stay next to the filter; a strategic merge patch
// adds the annotations, as a JSON patch add fails on a source without any.
func extrasBlock(chart string, sources []object, semverFilter string, annotations map[string]string) ([]string, error) {
	block := []string{fmt.Sprintf(blockBeginMarker, chart)}
	for _, s := range sources {
		target := []string{
			"- target:",
			"    kind: " + ociRepositoryKind,
			"    name: " + s.name(),
			"    namespace: " + s.namespace(),
			"  patch: |-",
		}

		ref, err := marshalYAML([]map[string]any{{
			"op":    "add",
			"path":  "/spec/ref",
			"value": map[string]any{"semver": devSemverRange, "semverFilter": semverFilter},
		}})
		if err != nil {
			return nil, microerror.Mask(err)
		}
		values := map[string]any{}
		for k, v := range annotations {
			values[k] = v
		}
		merge, err := marshalYAML(map[string]any{
			"apiVersion": s["apiVersion"],
			"kind":       ociRepositoryKind,
			"metadata": map[string]any{
				"name":        s.name(),
				"namespace":   s.namespace(),
				"annotations": values,
			},
			"spec": map[string]any{"interval": SourceInterval},
		})
		if err != nil {
			return nil, microerror.Mask(err)
		}

		for _, body := range [][]byte{ref, merge} {
			block = append(block, target...)
			for _, l := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
				block = append(block, "    "+l)
			}
		}
	}

	return append(block, fmt.Sprintf(blockEndMarker, chart)), nil
}

// writeBlock appends block, a list at column 0, to the root patches: list of
// the kustomization at path, after every existing entry. It edits the text, so
// removeBlock restores the file byte for byte. A block the chart already holds,
// from an expired reservation, is replaced.
func writeBlock(path, chart string, block []string) error {
	if hasBlock(path, chart) {
		if err := removeBlock(path, chart); err != nil {
			return microerror.Mask(err)
		}
	}
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}

	start := -1
	for i, l := range lines {
		if l == patchesKey || l == "patches: []" {
			lines[i] = patchesKey
			start = i
			break
		}
	}

	indent := "  "
	if start < 0 {
		lines = append(trimTrailingBlank(lines), patchesKey)
		start = len(lines) - 1
		lines = append(lines, "")
	}
	last := listEnd(lines, start)
	for i := start + 1; i <= last; i++ {
		if t := strings.TrimLeft(lines[i], " "); strings.HasPrefix(t, "- ") {
			indent = lines[i][:len(lines[i])-len(t)]
			break
		}
	}

	indented := make([]string, 0, len(block))
	for _, l := range block {
		indented = append(indented, indent+l)
	}
	lines = append(lines[:last+1], append(indented, lines[last+1:]...)...)

	return microerror.Mask(writeLines(path, lines))
}

// listEnd returns the index of the last line of the list under the key at
// start. Unlike blockEnd, it also takes items and comments at column 0, as
// kustomization files often write `- target:` without an indent.
func listEnd(lines []string, start int) (last int) {
	last = start
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.TrimSpace(l) == "":
			continue
		case strings.HasPrefix(l, " "), strings.HasPrefix(l, "\t"), strings.HasPrefix(l, "-"), strings.HasPrefix(l, "#"):
			last = i
		default:
			return last
		}
	}

	return last
}

// hasBlock reports whether the kustomization at path holds the chart's block.
func hasBlock(path, chart string) bool {
	lines, err := readLines(path)
	if err != nil {
		return false
	}
	_, _, ok := findBlock(lines, chart)

	return ok
}

func findBlock(lines []string, chart string) (begin, end int, ok bool) {
	begin = -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case fmt.Sprintf(blockBeginMarker, chart):
			begin = i
		case fmt.Sprintf(blockEndMarker, chart):
			if begin >= 0 {
				return begin, i, true
			}
		}
	}

	return 0, 0, false
}

// removeBlock removes the chart's block, reversing writeBlock exactly. When the
// block was the only entry, the patches: key goes too if writeBlock appended it
// at the end of the file, or goes back to `patches: []` when content follows.
func removeBlock(path, chart string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}
	begin, end, ok := findBlock(lines, chart)
	if !ok {
		return microerror.Maskf(componentNotFoundError, "%s holds no reservation block of %q", path, chart)
	}
	lines = append(lines[:begin], lines[end+1:]...)

	start := -1
	for i, l := range lines {
		if l == patchesKey {
			start = i
			break
		}
	}
	if start >= 0 && listEnd(lines, start) == start {
		if len(trimTrailingBlank(lines[start+1:])) == 0 {
			lines = append(trimTrailingBlank(lines[:start]), "")
		} else {
			lines[start] = "patches: []"
		}
	}

	return microerror.Mask(writeLines(path, lines))
}

// assertExtrasReserved is guard 1: every source of the chart comes out of the
// render on the dev builds. kustomize skips a patch whose target matches
// nothing without an error, so a render that succeeds proves nothing.
func assertExtrasReserved(objects []object, resolved extrasResolution, semverFilter string, annotations map[string]string) error {
	wantRef := map[string]any{"semver": devSemverRange, "semverFilter": semverFilter}
	for _, s := range resolved.sources {
		var got object
		for _, o := range objects {
			if o.kind() == ociRepositoryKind && o.name() == s.name() && o.namespace() == s.namespace() {
				got = o
				break
			}
		}
		if got == nil {
			return microerror.Maskf(renderAssertionError,
				"the rendered extras lost %s %s/%s", ociRepositoryKind, s.namespace(), s.name())
		}
		ref := fmt.Sprint(got.nested("spec", "ref"))
		if ref != fmt.Sprint(wantRef) || got.nested("spec", "interval") != SourceInterval {
			return microerror.Maskf(renderAssertionError,
				"the reservation block does not reach %s %s/%s in the rendered extras: it selects %s, want %v",
				ociRepositoryKind, s.namespace(), s.name(), ref, wantRef)
		}
		for k, v := range annotations {
			if got.nested("metadata", "annotations", k) != v {
				return microerror.Maskf(renderAssertionError,
					"the rendered %s %s/%s does not carry annotation %s=%q", ociRepositoryKind, s.namespace(), s.name(), k, v)
			}
		}
	}

	return nil
}
