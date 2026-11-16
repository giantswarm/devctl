package reservation

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
)

// checkExtras runs the guards an extras reservation needs before it writes
// anything. Guard 1, the render check, runs after the write.
func checkExtras(req Request, chart string, resolved extrasResolution) error {
	if err := checkOneTree(req, chart); err != nil {
		return microerror.Mask(err)
	}
	path := extrasKustomizationPath(req.RepoDir, req.Cluster)
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}
	if err := checkBlocksLast(lines, req.clusterPath(extrasDir, "kustomization.yaml")); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(checkNoHiddenReleases(req, resolved))
}

// CheckExtrasPatches is guard 2 for the extras kustomization at path: every
// reservation block comes after every other entry of the root patches: list. A
// patch after a block can change the source again, under the dev semverFilter,
// and then no dev build matches. The CI check of the GitOps repo runs it too.
func CheckExtrasPatches(path string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(checkBlocksLast(lines, path))
}

func checkBlocksLast(lines []string, name string) error {
	start := slices.Index(lines, patchesKey)
	if start < 0 {
		return nil
	}

	inBlock, afterBlock := false, false
	itemIndent := ""
	for i := start + 1; i <= listEnd(lines, start); i++ {
		t := strings.TrimLeft(lines[i], " ")
		switch {
		case strings.HasPrefix(t, "# BEGIN reservation of "):
			inBlock = true
		case strings.HasPrefix(t, "# END reservation of "):
			inBlock, afterBlock = false, true
		case strings.HasPrefix(t, "- "):
			indent := lines[i][:len(lines[i])-len(t)]
			if itemIndent == "" {
				itemIndent = indent
			}
			if indent == itemIndent && afterBlock && !inBlock {
				return microerror.Maskf(appNotSupportedError,
					"%s holds a patch at line %d after a reservation block. Reservation blocks must stay last in patches:, so that no later patch changes the reserved sources. Move the patch above the blocks",
					name, i+1)
			}
		}
	}

	return nil
}

// checkNoHiddenReleases is guard 3: a HelmRelease outside the extras render
// that takes its chart from a patched source moves too, and nobody sees it in
// the render. It reads every manifest of the cluster, not a render, as Flux
// Kustomization objects wire them up in ways no single overlay shows.
func checkNoHiddenReleases(req Request, resolved extrasResolution) error {
	patched := map[string]bool{}
	for _, s := range resolved.sources {
		patched[s.namespace()+"/"+s.name()] = true
	}

	var hidden []string
	root := filepath.Join(req.RepoDir, clustersDir, req.Cluster)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return microerror.Mask(err)
		}
		if ext := filepath.Ext(p); d.IsDir() || (ext != ".yaml" && ext != ".yml") {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // the path comes from walking the repo checkout
		if err != nil {
			return microerror.Mask(err)
		}
		objects, err := decodeObjects(b)
		if err != nil {
			return nil //nolint:nilerr // a file that is not plain YAML holds no release we can read
		}
		for _, o := range objects {
			if o.kind() != helmReleaseKind {
				continue
			}
			name, _ := o.nested("spec", "chartRef", "name").(string)
			namespace, _ := o.nested("spec", "chartRef", "namespace").(string)
			if namespace == "" {
				namespace = o.namespace()
			}
			release := o.namespace() + "/" + o.name()
			if namespace == "" || !patched[namespace+"/"+name] || slices.Contains(resolved.helmReleases, release) {
				continue
			}
			rel, _ := filepath.Rel(req.RepoDir, p)
			hidden = append(hidden, release+" in "+filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		return microerror.Mask(err)
	}
	if len(hidden) > 0 {
		sort.Strings(hidden)
		return microerror.Maskf(appNotSupportedError,
			"%s %s takes its chart from a source the reservation patches, but the extras render does not hold it, so it would move without anybody seeing it",
			helmReleaseKind, strings.Join(hidden, ", "))
	}

	return nil
}

// checkOneTree is guard 4: the chart is the key of a reservation, so it must
// be served from one tree of the cluster only.
func checkOneTree(req Request, chart string) error {
	collectionsPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, collectionsDir)
	if _, err := os.Stat(filepath.Join(collectionsPath, "kustomization.yaml")); os.IsNotExist(err) {
		return nil
	}
	objects, err := renderDir(collectionsPath)
	if err != nil {
		return microerror.Mask(err)
	}
	for _, o := range objects {
		if o.kind() != ociRepositoryKind || o.namespace() != Namespace || strings.HasSuffix(o.name(), SourceNameSuffix) {
			continue
		}
		if url, _ := o.nested("spec", "url").(string); chartFromURL(url) == chart {
			return microerror.Maskf(appAmbiguousError,
				"chart %q is served both from %s (%s %s) and from %s. A reservation is keyed on the chart, so it cannot tell which one to move",
				chart, req.clusterPath(collectionsDir), ociRepositoryKind, o.name(), req.clusterPath(extrasDir))
		}
	}

	return nil
}
