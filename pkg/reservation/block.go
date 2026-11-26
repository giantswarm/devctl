package reservation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
)

// A reservation, in the collections or the extras, adds no object: it patches
// the app's own sources in place, from one marked block at the end of the root
// patches: list of that tree's kustomization. Server-side apply drops the
// patched values again when the block goes away.

const (
	patchesKey      = "patches:"
	emptyPatchesKey = "patches: []"
	nullPatchesKey  = "patches: null"
	// patchesWasPrefix starts the line inside a block that records how the
	// patches: key looked before the first block, so removeBlock can restore it.
	patchesWasPrefix = "# patches: was "
	// patchesAbsent records a kustomization that had no patches: key at all.
	patchesAbsent = "absent"

	blockBeginMarker = "# BEGIN reservation of %s, written by `devctl reservation reserve`. Do not edit."
	blockEndMarker   = "# END reservation of %s, written by `devctl reservation reserve`"
)

// blockResolution is the app a reservation block moves: every source serving
// its chart, and every release taking its chart from one of them.
type blockResolution struct {
	// sources are the matched OCIRepository objects, sorted by namespace and name.
	sources []object
	// helmReleases are "<namespace>/<name>" of each moved release, sorted.
	helmReleases []string
	// chartRefs maps each moved release to its spec.chartRef before the block.
	chartRefs map[string]string
}

// chartRefOf is the spec.chartRef of a HelmRelease, as one comparable string.
func chartRefOf(o object) string {
	return fmt.Sprint(o.nested("spec", "chartRef"))
}

func (r blockResolution) sourceNames() []string {
	names := make([]string, 0, len(r.sources))
	for _, s := range r.sources {
		names = append(names, s.name())
	}

	return names
}

// sourceURL is the spec.url all sources share, or "" when they differ.
func (r blockResolution) sourceURL() string {
	var url string
	for i, s := range r.sources {
		u, _ := s.nested("spec", "url").(string)
		if i > 0 && u != url {
			return ""
		}
		url = u
	}

	return url
}

// resolveExtras finds the chart in a render of the cluster's extras. It reports
// false when the cluster has no extras, the extras serve no such chart, or no
// HelmRelease takes its chart from the sources that serve it.
func resolveExtras(req Request, chart string) (blockResolution, bool, error) {
	path := kustomizationPath(req.RepoDir, req.Cluster, GitOpsSourceExtras)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return blockResolution{}, false, nil
	}
	objects, err := renderDir(filepath.Dir(path))
	if err != nil {
		return blockResolution{}, false, microerror.Mask(err)
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
		return blockResolution{}, false, microerror.Maskf(appNotSupportedError,
			"%s %s in the extras of %s carries chart %q inline under spec.chart instead of referencing it with spec.chartRef. A reservation replaces a chart reference, so it cannot move it",
			helmReleaseKind, strings.Join(inline, ", "), req.Cluster, chart)
	}

	res := blockResolution{chartRefs: map[string]string{}}
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
		return blockResolution{}, false, nil
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
			res.chartRefs[o.namespace()+"/"+o.name()] = chartRefOf(o)
		}
	}
	sort.Strings(res.helmReleases)

	// Sources that move no HelmRelease leave the chart to the collections. The
	// caller refuses with movesNothingError only if the collections do not serve it.
	return res, len(res.helmReleases) > 0, nil
}

// movesNothingError refuses a chart whose extras sources no HelmRelease uses.
func (r blockResolution) movesNothingError(req Request, chart string) error {
	return microerror.Maskf(appNotFoundError,
		"the rendered extras of %s serve chart %q from %s %s, but no %s takes its chart from any of them, so a reservation would change nothing",
		req.Cluster, chart, ociRepositoryKind, strings.Join(r.sourceNames(), ", "), helmReleaseKind)
}

// reserveInPlace writes the reservation block into the kustomization of the
// GitOps source (extras or collections) and the ConfigMap entry, checks the
// render, and commits both.
func reserveInPlace(req Request, gitopsSource, chart string, resolved blockResolution, from, until time.Time, replaced *Reservation) (res Result, err error) {
	path := kustomizationPath(req.RepoDir, req.Cluster, gitopsSource)
	configMapPath := configMapFilePath(req.RepoDir, req.Cluster)
	semverFilter := devSemverFilter(req.Branch)
	annotations := reservationAnnotations(req, from, until)

	defer discardOnError(req.RepoDir, req.Cluster, gitopsSource, &err)

	var block []string
	if req.Bundle != "" {
		// The create or append choice must not count the chart's own old block,
		// which writeBlock replaces. discardOnError restores it on a failure.
		if hasBlock(path, chart) {
			if err = removeBlock(path, chart); err != nil {
				return Result{}, microerror.Mask(err)
			}
		}
		var before []object
		if before, err = renderDir(filepath.Dir(path)); err != nil {
			return Result{}, microerror.Mask(err)
		}
		block, err = bundleBlock(chart, before, resolved, semverFilter, annotations)
	} else {
		block, err = reservationBlock(chart, resolved.sources, semverFilter, annotations)
	}
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := writeBlock(path, chart, block); err != nil {
		return Result{}, microerror.Mask(err)
	}
	entry, err := reservationEntry(chart, req, gitopsSource, from, until)
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
	if req.Bundle != "" {
		err = assertBundleReserved(after, resolved, chart, semverFilter)
	} else {
		err = assertBlockReserved(after, resolved, semverFilter, annotations)
	}
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	files := blockFiles(req.Cluster, gitopsSource)
	sourceURL := resolved.sourceURL()
	if req.Bundle != "" {
		// resolved holds the bundle's source, not the member's.
		sourceURL = ""
	}

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
		GitOpsSource: gitopsSource,
		Bundle:       req.Bundle,
		SourceName:   strings.Join(resolved.sourceNames(), ", "),
		SourceURL:    sourceURL,
		HelmReleases: resolved.helmReleases,
		SemverFilter: semverFilter,
		Files:        files,
		From:         from,
		Until:        until,
		Replaced:     replaced,
	}, nil
}

// discardOnError restores the two files a reservation block edits when *err is
// set. Deferred by the functions that write them.
func discardOnError(repoDir, cluster, gitopsSource string, err *error) {
	if *err != nil {
		_ = runGit(context.Background(), repoDir, append([]string{"checkout", "HEAD", "--"}, blockFiles(cluster, gitopsSource)...)...)
	}
}

// blockFiles are the repo-relative paths a reservation block edits, sorted:
// the cluster's ConfigMap and the kustomization of the GitOps source.
func blockFiles(cluster, gitopsSource string) []string {
	files := []string{clusterPath(cluster, ConfigMapFile), kustomizationFile(cluster, gitopsSource)}
	sort.Strings(files)

	return files
}

// kustomizationFile is the repo-relative path of the kustomization of a GitOps
// source on a cluster.
func kustomizationFile(cluster, gitopsSource string) string {
	return clusterPath(cluster, gitopsSourceDir(gitopsSource), "kustomization.yaml")
}

// kustomizationPath is kustomizationFile under the working tree at repoDir.
func kustomizationPath(repoDir, cluster, gitopsSource string) string {
	return filepath.Join(repoDir, filepath.FromSlash(kustomizationFile(cluster, gitopsSource)))
}

// configMapFilePath is the path of the cluster's reservations ConfigMap under
// the working tree at repoDir.
func configMapFilePath(repoDir, cluster string) string {
	return filepath.Join(repoDir, clustersDir, cluster, ConfigMapFile)
}

// gitopsSourceDir is the cluster directory that holds a GitOps source.
func gitopsSourceDir(gitopsSource string) string {
	if gitopsSource == GitOpsSourceExtras {
		return extrasDir
	}

	return collectionsDir
}

// reservationBlock renders the reservation block as list items at column 0, enclosed
// in its markers. Per source, a JSON patch replaces spec.ref as a whole, so a
// stable tag or semver cannot stay next to the filter; a strategic merge patch
// adds the annotations, as a JSON patch add fails on a source without any.
func reservationBlock(chart string, sources []object, semverFilter string, annotations map[string]string) ([]string, error) {
	block := []string{fmt.Sprintf(blockBeginMarker, chart)}
	for _, s := range sources {
		target := []string{
			"- target:",
			"    kind: " + ociRepositoryKind,
			"    name: " + s.name(),
			"    namespace: " + s.namespace(),
			"  patch: |-",
		}

		ref, merge, err := sourcePatches(s.name(), s.namespace(), s["apiVersion"], semverFilter, annotations)
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

// sourcePatches are the two patch bodies that move a source to the dev builds:
// a JSON patch that replaces spec.ref as a whole, so a stable tag or semver
// cannot stay next to the filter, and a strategic merge patch that adds
// spec.interval and the annotations, as a JSON patch add fails on a source
// without any. Every reservation mode uses them, so a change is one change.
// A post-renderer patch picks its source by selector, so it passes the
// placeholder name that kustomize requires and ignores, with no namespace.
func sourcePatches(name, namespace string, apiVersion any, semverFilter string, annotations map[string]string) (ref, merge []byte, err error) {
	ref, err = marshalYAML([]map[string]any{{
		"op":    "add",
		"path":  "/spec/ref",
		"value": devRef(semverFilter),
	}})
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}
	values := map[string]any{}
	for k, v := range annotations {
		values[k] = v
	}
	metadata := map[string]any{"name": name, "annotations": values}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	merge, err = marshalYAML(map[string]any{
		"apiVersion": apiVersion,
		"kind":       ociRepositoryKind,
		"metadata":   metadata,
		"spec":       map[string]any{"interval": SourceInterval},
	})

	return ref, merge, microerror.Mask(err)
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

	// The first block records how the patches: key looked; later ones copy it.
	was := patchesAbsent
	start := slices.IndexFunc(lines, isPatchesKey)
	if start >= 0 {
		was = lines[start]
	}
	for _, l := range lines {
		if w, ok := strings.CutPrefix(strings.TrimSpace(l), patchesWasPrefix); ok {
			was = w
			break
		}
		// shortcut: a block from an older devctl records nothing, so the key
		// stays unrecorded, until the last block goes.
		if isBlockBegin(l) {
			was = ""
		}
	}
	if start >= 0 && (lines[start] == emptyPatchesKey || lines[start] == nullPatchesKey) {
		lines[start] = patchesKey
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

	if was != "" {
		block = append([]string{block[0], patchesWasPrefix + was}, block[1:]...)
	}
	indented := make([]string, 0, len(block))
	for _, l := range block {
		indented = append(indented, indent+l)
	}
	lines = append(lines[:last+1], append(indented, lines[last+1:]...)...)

	return microerror.Mask(writeLines(path, lines))
}

// isPatchesKey reports whether l is the root patches: key in any form a
// kustomization has it in.
func isPatchesKey(l string) bool {
	return l == patchesKey || l == emptyPatchesKey || l == nullPatchesKey
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

// gitopsSourceOf returns where the app's reservation lives on the cluster. The
// files decide, and list, extend and release all ask here.
func gitopsSourceOf(repoDir, cluster, app string) string {
	if hasBlock(kustomizationPath(repoDir, cluster, GitOpsSourceExtras), app) {
		return GitOpsSourceExtras
	}

	return GitOpsSourceCollections
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
// block was the only entry, the patches: key goes back to what the block
// recorded: gone, `patches: []`, `patches: null` or bare. A block without such
// a record, written by an older devctl, drops a key at the end of the file and
// empties one that content follows.
func removeBlock(path, chart string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}
	begin, end, ok := findBlock(lines, chart)
	if !ok {
		return microerror.Maskf(blockNotFoundError, "%s holds no reservation block of %q", path, chart)
	}
	was := ""
	for _, l := range lines[begin : end+1] {
		if w, ok := strings.CutPrefix(strings.TrimSpace(l), patchesWasPrefix); ok {
			was = w
		}
	}
	removed := slices.Clone(lines[begin : end+1])
	lines = append(lines[:begin], lines[end+1:]...)
	if err := promoteListCreators(lines, removed); err != nil {
		return microerror.Mask(err)
	}

	start := slices.IndexFunc(lines, isPatchesKey)
	if start >= 0 && listEnd(lines, start) == start {
		switch {
		case was == patchesAbsent, was == "" && len(trimTrailingBlank(lines[start+1:])) == 0:
			lines = append(trimTrailingBlank(lines[:start]), "")
		case was == "":
			lines[start] = emptyPatchesKey
		default:
			lines[start] = was
		}
	}

	return microerror.Mask(writeLines(path, lines))
}

// writeBlockAnnotations moves the hold in the chart's block: the annotations
// of the hold, once per source.
func writeBlockAnnotations(path, chart string, h hold) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}
	begin, end, ok := findBlock(lines, chart)
	if !ok {
		return microerror.Maskf(blockNotFoundError, "%s holds no reservation block of %q", path, chart)
	}
	if found := setWindowAnnotations(lines[begin:end+1], h); found == 0 || found%len(h.annotations()) != 0 {
		return microerror.Maskf(invalidConfigError, "the reservation block of %q in %s carries no %suser/%sfrom/%suntil annotations to update", chart, path, AnnotationPrefix, AnnotationPrefix, AnnotationPrefix)
	}

	return microerror.Mask(writeLines(path, lines))
}

// assertBlockReserved is guard 1: every source of the chart comes out of the
// render on the dev builds, and every release moved keeps its chartRef. kustomize skips a patch whose target matches
// nothing without an error, so a render that succeeds proves nothing.
func assertBlockReserved(objects []object, resolved blockResolution, semverFilter string, annotations map[string]string) error {
	wantRef := devRef(semverFilter)
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
				"the render lost %s %s/%s", ociRepositoryKind, s.namespace(), s.name())
		}
		ref := fmt.Sprint(got.nested("spec", "ref"))
		if ref != fmt.Sprint(wantRef) || got.nested("spec", "interval") != SourceInterval {
			return microerror.Maskf(renderAssertionError,
				"the reservation block does not reach %s %s/%s in the render: it selects %s, want %v",
				ociRepositoryKind, s.namespace(), s.name(), ref, wantRef)
		}
		for k, v := range annotations {
			if got.nested("metadata", "annotations", k) != v {
				return microerror.Maskf(renderAssertionError,
					"the rendered %s %s/%s does not carry annotation %s=%q", ociRepositoryKind, s.namespace(), s.name(), k, v)
			}
		}
	}

	for _, release := range resolved.helmReleases {
		var got object
		for _, o := range objects {
			if o.kind() == helmReleaseKind && o.namespace()+"/"+o.name() == release {
				got = o
				break
			}
		}
		if got == nil {
			return microerror.Maskf(renderAssertionError, "the render lost %s %s", helmReleaseKind, release)
		}
		if ref := chartRefOf(got); ref != resolved.chartRefs[release] {
			return microerror.Maskf(renderAssertionError,
				"the reservation block changed the chartRef of %s %s from %s to %s; a reservation must only change the source",
				helmReleaseKind, release, resolved.chartRefs[release], ref)
		}
	}

	return nil
}
