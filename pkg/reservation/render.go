package reservation

import (
	"bytes"
	"errors"
	"io"

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

// renderDir builds a cluster's collections overlay
// the way kustomize-controller does. A reservation that does not survive this render
// does nothing on the cluster, so it is the only trustworthy check available
// before the push.
func renderDir(dir string) ([]object, error) {
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
