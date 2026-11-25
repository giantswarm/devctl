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

// memberSelector is the label selector of the members of a chart.
func memberSelector(chart string) string {
	return memberChartLabel + "=" + chart
}

// bundleBlock renders the reservation block of a bundle member: per HelmRelease
// of the bundle, one JSON patch that appends the reservation post-renderer to
// spec.postRenderers, after any entry already there. The post-renderer patches
// the member OCIRepository with the same patches as every other mode.
func bundleBlock(chart string, before []object, resolved blockResolution, semverFilter string, annotations map[string]string) ([]string, error) {
	// A member source is not in the GitOps repo, so the apiVersion comes from
	// the bundle's own source, which the member shares.
	ref, merge, err := sourcePatches("not-used", "", resolved.sources[0]["apiVersion"], semverFilter, annotations)
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
