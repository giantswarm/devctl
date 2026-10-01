package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// Test_NewCreateValuesSchemaInput runs the real generate -> $ref fix -> normalize pipeline
// (see generateValuesSchema) against a small chart with no $k8s/ $ref aliases, so it never
// makes a network call and stays hermetic. It checks the pipeline produces valid, normalized
// (schemalint-canonical) JSON and is idempotent -- generating twice in a row must produce
// byte-identical output, which is exactly what makes the read-only pre-commit hook's diff
// check (see pre-commit-config.yaml.template) converge instead of flapping.
func Test_NewCreateValuesSchemaInput(t *testing.T) {
	dir := t.TempDir()
	chartDir := filepath.Join(dir, "helm", "test-chart")
	if err := os.MkdirAll(chartDir, 0750); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(
		"replicaCount: 1\nimage:\n  repository: nginx\n  tag: latest\n",
	), 0600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "zz_generated.app-platform.values.yaml"), []byte(
		"global:\n  podSecurityStandards:\n    enforced: \"\"\n",
	), 0600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	t.Chdir(dir)

	p := params.Params{K8sSchemaVersion: "v1.33.1"}
	in := NewCreateValuesSchemaInput(p, "test-chart")

	if want := filepath.Join("helm", "test-chart", "values.schema.json"); in.Path != want {
		t.Errorf("path: expected %q, got %q", want, in.Path)
	}
	if in.Generate == nil {
		t.Fatal("expected Generate to be set")
	}

	got, err := in.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(got, &schema); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}
	if schema["type"] != "object" {
		t.Errorf(`expected top-level "type": "object", got %v`, schema["type"])
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top-level \"properties\" object, got %v", schema["properties"])
	}
	if _, ok := props["replicaCount"]; !ok {
		t.Errorf("expected replicaCount in properties, got %v", props)
	}

	// Idempotent: running Generate again against the same on-disk inputs must produce the
	// exact same bytes, since that is the fixed point the read-only pre-commit hook's diff
	// check relies on.
	again, err := in.Generate(context.Background())
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if string(got) != string(again) {
		t.Errorf("Generate is not idempotent:\nfirst:  %s\nsecond: %s", got, again)
	}
}
