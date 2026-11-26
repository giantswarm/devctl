package reservation

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// checkBlock runs the guards a reservation block needs before it writes
// anything, in the kustomization of gitopsSource. Guard 1, the render check,
// runs after the write.
func checkBlock(req Request, gitopsSource, chart string, resolved blockResolution) error {
	if gitopsSource == GitOpsSourceExtras {
		if err := checkOneTree(req, chart); err != nil {
			return microerror.Mask(err)
		}
	}
	lines, err := readLines(kustomizationPath(req.RepoDir, req.Cluster, gitopsSource))
	if err != nil {
		return microerror.Mask(err)
	}
	name := kustomizationFile(req.Cluster, gitopsSource)
	if err := checkBlocksLast(lines, name); err != nil {
		return microerror.Mask(err)
	}
	if err := checkNoLaterTransforms(lines, name); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(checkNoHiddenReleases(req, gitopsSource, resolved))
}

// CheckBlockPatches is guard 2 for the extras or collections kustomization at path: every
// reservation block comes after every other entry of the root patches: list. A
// patch after a block can change the source again, under the dev semverFilter,
// and then no dev build matches. The CI check of the GitOps repo runs it too.
func CheckBlockPatches(path string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}

	if err := checkBlocksLast(lines, path); err != nil {
		return microerror.Mask(err)
	}
	if !slices.ContainsFunc(lines, isBlockBegin) {
		return nil
	}

	if err := checkNoLaterTransforms(lines, path); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(checkBundleBlocks(path, lines))
}

// overrideFS reads one file as content, whatever the disk holds.
type overrideFS struct {
	filesys.FileSystem
	path    string
	content []byte
}

func (f overrideFS) ReadFile(path string) ([]byte, error) {
	if abs, err := filepath.Abs(path); err == nil && abs == f.path {
		return f.content, nil
	}

	return f.FileSystem.ReadFile(path)
}

// checkBundleBlocks is the CI side of the bundle guards, for a kustomization
// that holds bundle blocks. It renders the directory of path with the blocks, and
// refuses a post-renderer after a reservation one. It renders it again without
// the blocks, and refuses a block that creates the post-renderers of a
// HelmRelease that has some of its own: the create block replaces them, and
// the reserve that wrote it chose that form when the list was empty.
func checkBundleBlocks(path string, lines []string) error {
	ops := postRendererOps(lines)
	if len(ops) == 0 {
		return nil
	}
	dir := filepath.Dir(path)
	with, err := renderDir(dir)
	if err != nil {
		return microerror.Mask(err)
	}
	if err := checkPostRenderersLast(with, path); err != nil {
		return microerror.Mask(err)
	}

	var plain []string
	for i, in := range inBlock(lines) {
		if !in {
			plain = append(plain, lines[i])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return microerror.Mask(err)
	}
	without, err := renderFS(overrideFS{filesys.MakeFsOnDisk(), abs, []byte(strings.Join(plain, "\n"))}, dir)
	if err != nil {
		return microerror.Mask(err)
	}
	for _, op := range ops {
		if !op.creates {
			continue
		}
		namespace, name, _ := strings.Cut(op.release, "/")
		if hasPostRenderers(without, namespace, name) {
			return microerror.Maskf(appNotSupportedError,
				"%s: the reservation block of %s %s creates spec.postRenderers, but the %s has post-renderers of its own, which the block would replace. Run `devctl reservation reserve` again for the member (a refresh), which chooses the form again, or release the reservation",
				path, helmReleaseKind, op.release, helmReleaseKind)
		}
	}

	return nil
}

// inBlock marks the lines of the reservation blocks, from the begin marker to
// the end marker, both included.
func inBlock(lines []string) []bool {
	in := make([]bool, len(lines))
	open := false
	for i, l := range lines {
		open = open || isBlockBegin(l)
		in[i] = open
		open = open && !isBlockEnd(l)
	}

	return in
}

func isBlockBegin(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), "# BEGIN reservation of ")
}

func isBlockEnd(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), "# END reservation of ")
}

// transformTarget is the selector of a patchesJson6902 or replacements entry.
type transformTarget struct {
	Kind string `yaml:"kind"`
}

// reachesSource is true for a selector that can match an OCIRepository: one
// without a kind matches every kind.
func (t transformTarget) reachesSource() bool {
	return t.Kind == "" || t.Kind == ociRepositoryKind
}

// checkNoLaterTransforms is the rest of guard 2: kustomize runs
// patchesJson6902, replacements and transformers after the patches: list, so
// one that can reach an OCIRepository can change a reserved source after its
// block. A replacement read from a file and a transformer can reach anything.
func checkNoLaterTransforms(lines []string, name string) error {
	var k struct {
		PatchesJson6902 []struct {
			Target transformTarget `yaml:"target"`
		} `yaml:"patchesJson6902"`
		Replacements []struct {
			Path    string `yaml:"path"`
			Targets []struct {
				Select transformTarget `yaml:"select"`
			} `yaml:"targets"`
		} `yaml:"replacements"`
		Transformers []any `yaml:"transformers"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &k); err != nil {
		return microerror.Maskf(invalidConfigError, "parsing %s: %v", name, err)
	}

	field := ""
	for _, p := range k.PatchesJson6902 {
		if p.Target.reachesSource() {
			field = "patchesJson6902"
		}
	}
	for _, r := range k.Replacements {
		reaches := r.Path != ""
		for _, t := range r.Targets {
			reaches = reaches || t.Select.reachesSource()
		}
		if reaches {
			field = "replacements"
		}
	}
	if len(k.Transformers) > 0 {
		field = "transformers"
	}
	if field != "" {
		return microerror.Maskf(appNotSupportedError,
			"%s holds %s that can change an %s. kustomize runs it after the patches: list, so it can change the reserved sources after the reservation blocks. Move it into patches:, above the blocks",
			name, field, ociRepositoryKind)
	}

	return nil
}

func checkBlocksLast(lines []string, name string) error {
	start := slices.IndexFunc(lines, isPatchesKey)
	if start < 0 {
		return nil
	}

	in := inBlock(lines)
	afterBlock := false
	itemIndent := ""
	for i := start + 1; i <= listEnd(lines, start); i++ {
		t := strings.TrimLeft(lines[i], " ")
		switch {
		case isBlockEnd(t):
			afterBlock = true
		case strings.HasPrefix(t, "- "):
			indent := lines[i][:len(lines[i])-len(t)]
			if itemIndent == "" {
				itemIndent = indent
			}
			if indent == itemIndent && afterBlock && !in[i] {
				return microerror.Maskf(appNotSupportedError,
					"%s holds a patch at line %d after a reservation block. Reservation blocks must stay last in patches:, so that no later patch changes the reserved sources. Move the patch above the blocks",
					name, i+1)
			}
		}
	}

	return nil
}

// checkNoHiddenReleases is guard 3: a HelmRelease outside the render of the tree
// that takes its chart from a patched source moves too, and nobody sees it in
// the render. It reads every manifest of the cluster, not a render, as Flux
// Kustomization objects wire them up in ways no single overlay shows.
func checkNoHiddenReleases(req Request, gitopsSource string, resolved blockResolution) error {
	patched, patchedNames := map[string]bool{}, map[string]bool{}
	for _, s := range resolved.sources {
		patched[s.namespace()+"/"+s.name()] = true
		patchedNames[s.name()] = true
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
			// A file without a namespace gets it from a kustomization or a Flux
			// Kustomization that guard 3 does not read, so it matches on names.
			usesPatched := patched[namespace+"/"+name]
			if namespace == "" {
				usesPatched = patchedNames[name]
			}
			release := o.namespace() + "/" + o.name()
			rendered := slices.Contains(resolved.helmReleases, release)
			if o.namespace() == "" {
				release = o.name()
				rendered = slices.ContainsFunc(resolved.helmReleases, func(r string) bool { return strings.HasSuffix(r, "/"+release) })
			}
			if !usesPatched || rendered {
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
			"%s %s takes its chart from a source the reservation patches, but the %s render does not hold it, so it would move without anybody seeing it",
			helmReleaseKind, strings.Join(hidden, ", "), gitopsSource)
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
		if o.kind() != ociRepositoryKind || o.namespace() != Namespace {
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

// checkNoOldReservation refuses a chart that an older devctl reserved with a
// component under collections/reservations/<chart>. This devctl writes and
// removes blocks only, so it can neither release that reservation nor reserve
// the chart beside it.
func checkNoOldReservation(repoDir, cluster, chart string) error {
	dir := clusterPath(cluster, collectionsDir, "reservations", chart)
	if _, err := os.Stat(filepath.Join(repoDir, dir)); err != nil {
		return nil //nolint:nilerr // no component, no old reservation
	}

	return microerror.Maskf(appNotSupportedError,
		"%s holds a reservation of %q made by an older devctl. Release it with that older devctl first",
		dir, chart)
}
