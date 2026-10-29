package file

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"text/template"

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

// Test_NewCreateValuesSchemaInput_appPlatformValues runs the rendered app-platform values
// file through the generator. Every value the platform injects must end up in the schema:
// an App of external-dns-app 3.7.x, whose chart closes its own `global`, failed on alba with
// "additional properties 'managementCluster' not allowed" and "at '/global': additional
// properties 'podSecurityStandards' not allowed". A chart that closes `global` keeps it
// closed for keys nobody declared, and a chart without a `global` of its own keeps the open,
// nullable `global` it had before the file declared one.
func Test_NewCreateValuesSchemaInput_appPlatformValues(t *testing.T) {
	var appPlatformValues bytes.Buffer
	in := NewCreateAppPlatformValuesInput(params.Params{}, "test-chart")
	if err := template.Must(template.New("").Parse(in.TemplateBody)).Execute(&appPlatformValues, in.TemplateData); err != nil {
		t.Fatalf("render app-platform values: %v", err)
	}

	tests := []struct {
		name             string
		values           string
		wantGlobalOpen   bool
		wantGlobalTypes  []string
		wantGlobalFields []string
	}{
		{
			name:             "chart closes global",
			values:           "global:  # @schema additionalProperties: false\n  imagePullSecrets: []\nreplicaCount: 1\n",
			wantGlobalOpen:   false,
			wantGlobalTypes:  []string{"object"},
			wantGlobalFields: []string{"imagePullSecrets", "podSecurityStandards"},
		},
		{
			name:             "chart without global",
			values:           "replicaCount: 1\n",
			wantGlobalOpen:   true,
			wantGlobalTypes:  []string{"object", "null"},
			wantGlobalFields: []string{"podSecurityStandards"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			chartDir := filepath.Join(dir, "helm", "test-chart")
			if err := os.MkdirAll(chartDir, 0750); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(tc.values), 0600); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			if err := os.WriteFile(filepath.Join(chartDir, "zz_generated.app-platform.values.yaml"), appPlatformValues.Bytes(), 0600); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			t.Chdir(dir)

			got, err := NewCreateValuesSchemaInput(params.Params{K8sSchemaVersion: "v1.33.1"}, "test-chart").Generate(context.Background())
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}

			var schema struct {
				AdditionalProperties *bool `json:"additionalProperties"`
				Properties           map[string]struct {
					Type                 json.RawMessage            `json:"type"`
					AdditionalProperties *bool                      `json:"additionalProperties"`
					Properties           map[string]json.RawMessage `json:"properties"`
				} `json:"properties"`
			}
			if err := json.Unmarshal(got, &schema); err != nil {
				t.Fatalf("output is not valid JSON: %v\n%s", err, got)
			}

			if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
				t.Errorf("expected the root to stay closed, got additionalProperties %v", schema.AdditionalProperties)
			}
			for _, key := range []string{"externalDNSIP", "helm", "managementCluster", "replicaCount"} {
				if _, ok := schema.Properties[key]; !ok {
					t.Errorf("expected %q in properties", key)
				}
			}

			global, ok := schema.Properties["global"]
			if !ok {
				t.Fatalf("expected global in properties\n%s", got)
			}
			if global.AdditionalProperties == nil {
				t.Errorf("expected global additionalProperties %v, got none", tc.wantGlobalOpen)
			} else if *global.AdditionalProperties != tc.wantGlobalOpen {
				t.Errorf("expected global additionalProperties %v, got %v", tc.wantGlobalOpen, *global.AdditionalProperties)
			}
			var types []string
			if err := json.Unmarshal(global.Type, &types); err != nil {
				var single string
				if err := json.Unmarshal(global.Type, &single); err != nil {
					t.Fatalf("global type %s: %v", global.Type, err)
				}
				types = []string{single}
			}
			if !slices.Equal(types, tc.wantGlobalTypes) {
				t.Errorf("expected global type %v, got %v", tc.wantGlobalTypes, types)
			}
			for _, key := range tc.wantGlobalFields {
				if _, ok := global.Properties[key]; !ok {
					t.Errorf("expected %q in global.properties\n%s", key, got)
				}
			}
		})
	}
}
