package reservation

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
)

// memberChartLabel is the label every member OCIRepository of a bundle carries,
// with the chart that member serves as its value. The reservation post-renderer
// selects on it, so every member of the chart moves together.
const memberChartLabel = "bundle.giantswarm.io/member-chart"

// ignoredName is the metadata.name of a post-renderer strategic merge patch.
// kustomize requires one, and ignores it when the patch has a target selector.
const ignoredName = "not-used"

// memberSelector is the label selector of the members of a chart.
func memberSelector(chart string) string {
	return memberChartLabel + "=" + chart
}

// bundleBlock renders the reservation block of a bundle member: per HelmRelease
// of the bundle, one JSON patch that appends the reservation post-renderer to
// spec.postRenderers, after any entry already there. The post-renderer patches
// the member OCIRepository with the same patches as every other mode.
func bundleBlock(chart string, before []object, resolved blockResolution, semverFilter string, annotations map[string]string) ([]string, error) {
	// checkBundleChart has refused a bundle with no source by now.
	// A member source is not in the GitOps repo, so the apiVersion comes from
	// the bundle's own source, which the member shares.
	ref, merge, err := sourcePatches(ignoredName, "", resolved.sources[0]["apiVersion"], semverFilter, annotations)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	target := map[string]any{"kind": ociRepositoryKind, "labelSelector": memberSelector(chart)}
	postRenderer := map[string]any{"kustomize": map[string]any{"patches": []map[string]any{
		{"target": target, "patch": strings.TrimRight(string(ref), "\n")},
		{"target": target, "patch": strings.TrimRight(string(merge), "\n")},
	}}}

	block := []string{fmt.Sprintf(blockBeginMarker, chart)}
	for _, release := range resolved.helmReleases {
		namespace, name, _ := strings.Cut(release, "/")
		op := map[string]any{"op": "add", "path": "/spec/postRenderers/-", "value": postRenderer}
		// JSON patch add appends only to a list that exists.
		if !hasPostRenderers(before, namespace, name) {
			op["path"], op["value"] = "/spec/postRenderers", []any{postRenderer}
		}
		body, err := marshalYAML([]map[string]any{op})
		if err != nil {
			return nil, microerror.Mask(err)
		}
		block = append(block,
			"- target:",
			"    kind: "+helmReleaseKind,
			"    name: "+name,
			"    namespace: "+namespace,
			"  patch: |-")
		for _, l := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
			block = append(block, "    "+l)
		}
	}

	return append(block, fmt.Sprintf(blockEndMarker, chart)), nil
}

// hasPostRenderers reports whether the render holds the HelmRelease with a
// spec.postRenderers list, which a JSON patch can append to.
func hasPostRenderers(objects []object, namespace, name string) bool {
	for _, o := range objects {
		if o.kind() == helmReleaseKind && o.namespace() == namespace && o.name() == name {
			_, ok := o.nested("spec", "postRenderers").([]any)

			return ok
		}
	}

	return false
}

// assertBundleReserved is guard 1 for a bundle member: every HelmRelease of the
// bundle comes out of the render with the reservation post-renderer last, with
// the member selector and the dev ref. kustomize skips a patch whose target
// matches nothing, so a render that succeeds proves nothing.
func assertBundleReserved(objects []object, resolved blockResolution, chart, semverFilter string) error {
	want := fmt.Sprint(devRef(semverFilter))
	for _, release := range resolved.helmReleases {
		var got object
		for _, o := range objects {
			if o.kind() == helmReleaseKind && o.namespace()+"/"+o.name() == release {
				got = o
			}
		}
		if got == nil {
			return microerror.Maskf(renderAssertionError, "the render lost %s %s", helmReleaseKind, release)
		}
		entries, _ := got.nested("spec", "postRenderers").([]any)
		if len(entries) == 0 {
			return microerror.Maskf(renderAssertionError,
				"the reservation block does not reach %s %s in the render: it carries no post-renderer", helmReleaseKind, release)
		}
		last, _ := entries[len(entries)-1].(map[string]any)
		patches, _ := object(last).nested("kustomize", "patches").([]any)
		selected, refSet := 0, false
		for _, p := range patches {
			patch, _ := p.(map[string]any)
			target, _ := patch["target"].(map[string]any)
			if target["kind"] == ociRepositoryKind && target["labelSelector"] == memberSelector(chart) {
				selected++
			}
			text, _ := patch["patch"].(string)
			var ops []struct {
				Path  string `yaml:"path"`
				Value any    `yaml:"value"`
			}
			if yaml.Unmarshal([]byte(text), &ops) == nil && len(ops) == 1 && ops[0].Path == "/spec/ref" && fmt.Sprint(ops[0].Value) == want {
				refSet = true
			}
		}
		if selected == 0 || selected != len(patches) || !refSet {
			return microerror.Maskf(renderAssertionError,
				"the last post-renderer of %s %s does not select %s %s and set the dev ref %s",
				helmReleaseKind, release, ociRepositoryKind, memberSelector(chart), want)
		}
	}

	return nil
}

// postRendererOp is one bundle block patch on a HelmRelease's spec.postRenderers,
// read back from the text of a kustomization.
type postRendererOp struct {
	release string // "<namespace>/<name>"
	line    int    // index of the `path:` line
	creates bool   // true: the patch creates the list, false: it appends to it
}

// postRendererOps lists the post-renderer patches of the reservation blocks in
// lines, in file order.
func postRendererOps(lines []string) []postRendererOp {
	var ops []postRendererOp
	in, release := inBlock(lines), ""
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case in[i] && t == "kind: "+helmReleaseKind && i+2 < len(lines):
			name := strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "name: ")
			namespace := strings.TrimPrefix(strings.TrimSpace(lines[i+2]), "namespace: ")
			release = namespace + "/" + name
		case in[i] && (t == "path: /spec/postRenderers" || t == "path: /spec/postRenderers/-"):
			ops = append(ops, postRendererOp{release: release, line: i, creates: t == "path: /spec/postRenderers"})
		}
	}

	return ops
}

// promoteListCreators keeps the post-renderers of the blocks that remain valid
// after removed, the lines of a removed block, went away. JSON patch can append
// only to a list that exists, and the first block of a HelmRelease is the one
// that creates it. When that block goes, the first remaining block of the same
// HelmRelease takes over, with the same entry in it.
func promoteListCreators(lines, removed []string) error {
	for _, gone := range postRendererOps(removed) {
		if !gone.creates {
			continue
		}
		for _, op := range postRendererOps(lines) {
			if op.release != gone.release {
				continue
			}
			if !op.creates {
				if err := promoteToListCreator(lines, op.line); err != nil {
					return microerror.Mask(err)
				}
			}

			break
		}
	}

	return nil
}

// promoteToListCreator rewrites the append patch whose path is at line into the
// one that creates the list: the path loses `/-` and the entry becomes the one
// item of a list.
func promoteToListCreator(lines []string, line int) error {
	value := line + 1
	if value+1 >= len(lines) || strings.TrimSpace(lines[value]) != "value:" {
		return microerror.Maskf(invalidConfigError, "line %d of the kustomization is not the `value:` of a reservation post-renderer patch. Edit the block by hand or release it before the other one", value+1)
	}
	valueIndent := len(lines[value]) - len(strings.TrimLeft(lines[value], " "))
	for i := value + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if indent <= valueIndent {
			break
		}
		if i == value+1 {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "- ") {
				return microerror.Maskf(invalidConfigError, "line %d of the kustomization is a list, not the mapping a reservation post-renderer patch holds", i+1)
			}
			lines[i] = lines[i][:indent] + "- " + lines[i][indent:]
		} else {
			lines[i] = "  " + lines[i]
		}
	}
	lines[line] = strings.TrimSuffix(lines[line], "/-")

	return nil
}

// isReservationPostRenderer reports whether a post-renderer entry is one that a
// bundle block wrote: it patches only the OCIRepository members of a chart.
func isReservationPostRenderer(entry any) bool {
	e, _ := entry.(map[string]any)
	patches, _ := object(e).nested("kustomize", "patches").([]any)
	if len(patches) == 0 {
		return false
	}
	for _, p := range patches {
		target, _ := p.(map[string]any)["target"].(map[string]any)
		selector, _ := target["labelSelector"].(string)
		if target["kind"] != ociRepositoryKind || !strings.HasPrefix(selector, memberChartLabel+"=") {
			return false
		}
	}

	return true
}

// checkPostRenderersLast is the post-renderer guard of a bundle: no entry that is
// not a reservation one comes after a reservation one in the spec.postRenderers
// of a rendered HelmRelease. A bundle block appends to the list, so such an
// entry can come only from something that runs after the block, and it would
// see the sources the reservation already moved.
func checkPostRenderersLast(objects []object, name string) error {
	for _, o := range objects {
		if o.kind() != helmReleaseKind {
			continue
		}
		entries, _ := o.nested("spec", "postRenderers").([]any)
		reserved := -1
		for i, e := range entries {
			switch {
			case isReservationPostRenderer(e):
				if reserved < 0 {
					reserved = i + 1
				}
			case reserved > 0:
				return microerror.Maskf(appNotSupportedError,
					"%s: %s %s/%s has post-renderer %d after a reservation post-renderer (%d). Reservation post-renderers must stay last in spec.postRenderers. Add the post-renderer to the %s itself, or to a patch that runs before the reservation blocks, not to a patchesJson6902 or a transformer, which run after them",
					name, helmReleaseKind, o.namespace(), o.name(), i+1, reserved, helmReleaseKind)
			}
		}
	}

	return nil
}
