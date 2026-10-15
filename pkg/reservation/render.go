package reservation

import (
	"bytes"
	"errors"
	"io"
	"reflect"

	"github.com/giantswarm/microerror"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

const (
	ociRepositoryKind = "OCIRepository"
	helmReleaseKind   = "HelmRelease"
)

// object is one rendered Kubernetes object. Plain maps, not typed structs: the
// reservation copies a source object field for field, and a typed round-trip
// would drop every field the type does not know.
type object map[string]any

func (o object) kind() string { s, _ := o["kind"].(string); return s }

func (o object) metadata() map[string]any {
	m, _ := o["metadata"].(map[string]any)
	return m
}

func (o object) name() string {
	s, _ := o.metadata()["name"].(string)
	return s
}

func (o object) namespace() string {
	s, _ := o.metadata()["namespace"].(string)
	return s
}

// nested walks a path of map keys. It returns nil when any step is missing or
// is not a map.
func (o object) nested(path ...string) any {
	var cur any = map[string]any(o)
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[key]
		if !ok {
			return nil
		}
	}

	return cur
}

// renderCollections builds the cluster's collections overlay the way
// kustomize-controller does. A reservation that does not survive this render
// does nothing on the cluster, so it is the only trustworthy check available
// before the push.
func renderCollections(dir string) ([]object, error) {
	m, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		return nil, microerror.Maskf(renderError, "rendering %s: %v", dir, err)
	}

	out, err := m.AsYaml()
	if err != nil {
		return nil, microerror.Maskf(renderError, "serialising the render of %s: %v", dir, err)
	}

	objects, err := decodeObjects(out)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return objects, nil
}

// findSource returns the OCIRepository with the given name. It is a lookup of a
// name the reservation itself chose, never the way an app is located: an app is
// resolved on the chart its URL serves, in resolveApp.
func findSource(objects []object, name string) (object, bool) {
	for _, o := range objects {
		if o.kind() == ociRepositoryKind && o.namespace() == Namespace && o.name() == name {
			return o, true
		}
	}

	return nil, false
}

// assertReserved refuses a render that does not carry the reservation. A render
// that only succeeds proves nothing: a component merged in the wrong order
// leaves the cluster on its release version with no error anywhere.
func assertReserved(objects []object, resolved resolution, sourceName, semverFilter string) error {
	source, ok := findSource(objects, sourceName)
	if !ok {
		return microerror.Maskf(renderAssertionError,
			"the rendered collections carry no %s named %q", ociRepositoryKind, sourceName)
	}
	if got, _ := source.nested("spec", "ref", "semverFilter").(string); got != semverFilter {
		return microerror.Maskf(renderAssertionError,
			"the rendered %s %q selects %q, want %q", ociRepositoryKind, sourceName, got, semverFilter)
	}

	// Every instance has to follow the reservation. One instance left behind is
	// a cluster running two versions of one app with no error anywhere.
	patched := map[string]bool{}
	for _, o := range objects {
		if o.kind() != helmReleaseKind || o.namespace() != Namespace {
			continue
		}
		if name, _ := o.nested("spec", "chartRef", "name").(string); name == sourceName {
			patched[o.name()] = true
		}
	}
	for _, name := range resolved.helmReleases {
		if !patched[name] {
			return microerror.Maskf(renderAssertionError,
				"%s %q does not take its chart from %q in the rendered collections, so the reservation would change nothing for that instance",
				helmReleaseKind, name, sourceName)
		}
	}

	// The release and release-candidate version selection has to keep working
	// untouched, so every source object the app is served from must come out of
	// the render exactly as it went in.
	for _, name := range resolved.sourceNames {
		original, ok := findSource(objects, name)
		if !ok {
			return microerror.Maskf(renderAssertionError,
				"the reservation removed the app's own %s %q from the render", ociRepositoryKind, name)
		}
		if got := original.nested("spec", "ref"); !reflect.DeepEqual(got, resolved.originalRefs[name]) {
			return microerror.Maskf(renderAssertionError,
				"the reservation changed the version selector of %q from %v to %v", name, resolved.originalRefs[name], got)
		}
	}

	return nil
}

// decodeObjects splits a multi-document YAML stream into objects.
func decodeObjects(out []byte) ([]object, error) {
	var objects []object
	dec := yaml.NewDecoder(bytes.NewReader(out))
	for {
		// Decoded into a plain map, never into object: yaml.v3 gives every
		// nested mapping the type of the map it decodes into, and a nested
		// object would then fail every map[string]any type assertion below.
		var o map[string]any
		err := dec.Decode(&o)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, microerror.Maskf(renderError, "parsing the render: %v", err)
		}
		if len(o) > 0 {
			objects = append(objects, object(o))
		}
	}

	return objects, nil
}
