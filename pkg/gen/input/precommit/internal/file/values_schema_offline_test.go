package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// Test_NewCreateValuesSchemaInput_unreachableSchemaHost checks what happens when the
// Kubernetes schema host cannot be reached. A values.yaml that carries a "$ref: $k8s/..."
// annotation makes the bundler fetch that URL over the network, so the generation fails
// when the host is down or the machine is offline. devctl must report the failure and name
// the URL it could not read, instead of writing a truncated values.schema.json. See
// giantswarm/devctl#2195.
func Test_NewCreateValuesSchemaInput_unreachableSchemaHost(t *testing.T) {
	// Port 1 on the loopback interface refuses the connection at once: no DNS lookup, no
	// timeout, and the test needs no network.
	const unreachable = "http://127.0.0.1:1/%s/"

	orig := k8sSchemaURLFormat
	k8sSchemaURLFormat = unreachable
	t.Cleanup(func() { k8sSchemaURLFormat = orig })

	dir := t.TempDir()
	chartDir := filepath.Join(dir, "helm", "test-chart")
	if err := os.MkdirAll(chartDir, 0755); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(
		"resources: {} # @schema $ref: $k8s/_definitions.json#/definitions/io.k8s.api.core.v1.ResourceRequirements\n",
	), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "zz_generated.app-platform.values.yaml"), []byte(
		"global:\n  podSecurityStandards:\n    enforced: \"\"\n",
	), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Not the default v1.33.1: the version in the URL must come from the parameter.
	p := params.Params{K8sSchemaVersion: "v1.29.0"}
	in := NewCreateValuesSchemaInput(p, "test-chart")

	got, err := in.Generate(t.Context())
	if err == nil {
		t.Fatalf("expected an error, got a schema:\n%s", got)
	}
	// The whole URL, not only the host: an operator who reads a failed `devctl gen
	// precommit` must see which document devctl could not read.
	const wantURL = "http://127.0.0.1:1/v1.29.0/_definitions.json"
	if !strings.Contains(err.Error(), wantURL) {
		t.Errorf("error does not name %q: %v", wantURL, err)
	}
}
