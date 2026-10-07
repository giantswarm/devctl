package reservation_test

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// renderCluster builds the cluster's collections the way kustomize-controller
// does and returns the objects by "<kind>/<name>". It is how the tests ask what
// the cluster would actually run.
func renderCluster(t *testing.T, dir, cluster string) map[string]map[string]any {
	t.Helper()

	target := filepath.Join(dir, "management-clusters", cluster, "collections")
	res, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(filesys.MakeFsOnDisk(), target)
	if err != nil {
		t.Fatalf("rendering %s: %v", target, err)
	}
	out, err := res.AsYaml()
	if err != nil {
		t.Fatal(err)
	}

	objects := map[string]map[string]any{}
	dec := yaml.NewDecoder(bytes.NewReader(out))
	for {
		var o map[string]any
		err := dec.Decode(&o)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("parsing the render of %s: %v", target, err)
		}
		if len(o) == 0 {
			continue
		}
		metadata, _ := o["metadata"].(map[string]any)
		objects[o["kind"].(string)+"/"+metadata["name"].(string)] = o
	}

	return objects
}

// mustObject returns one rendered object, failing the test when the render does
// not carry it.
func mustObject(t *testing.T, objects map[string]map[string]any, key string) map[string]any {
	t.Helper()

	o, ok := objects[key]
	if !ok {
		t.Fatalf("the render carries no %s", key)
	}

	return o
}
