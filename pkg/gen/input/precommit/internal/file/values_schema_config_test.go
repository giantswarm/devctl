package file

import (
	"bytes"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
	"github.com/giantswarm/devctl/v8/pkg/gen/internal"
)

// Test_newSchemaGenConfig_matchesSchemaYamlTemplate is the drift guard between the two
// copies of the helm-values-schema-json config: the Go literal newSchemaGenConfig builds
// and schema.yaml.template, which the read-only pre-commit hook feeds to the same library
// via --config. They have to describe the same generation forever, and nothing else
// enforces it -- change one side only and devctl's tests stay green while every chart's
// hook fails with an opaque schema diff.
//
// Params.Dir stays empty here because the template's paths are relative to the chart
// repository root, which is what Dir points at.
func Test_newSchemaGenConfig_matchesSchemaYamlTemplate(t *testing.T) {
	testCases := []struct {
		name      string
		p         params.Params
		chartName string
	}{
		{
			name:      "case 1: basic chart",
			p:         params.Params{Dir: "", K8sSchemaVersion: "v1.33.1"},
			chartName: "my-app",
		},
		{
			name:      "case 2: chart name with a dash",
			p:         params.Params{Dir: "", K8sSchemaVersion: "v1.29.0"},
			chartName: "platform-chart",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := internal.Execute(t.Context(), &buf, NewCreateSchemaYamlInput(tc.p, tc.chartName)); err != nil {
				t.Fatalf("render .schema.yaml: %v", err)
			}

			var rendered map[string]interface{}
			if err := yaml.Unmarshal(buf.Bytes(), &rendered); err != nil {
				t.Fatalf("unmarshal .schema.yaml: %v\n%s", err, buf.String())
			}

			// The output path is the one field that legitimately differs. The Go path
			// generates into a scratch file and post-processes it ($ref fix, normalize)
			// before anything reaches helm/<chart>/values.schema.json, and the hook
			// overrides output: with -o "$tmp" for the same reason. So the scratch path
			// below is arbitrary, and what the template's output: has to match is the
			// file devctl actually commits.
			cfg := newSchemaGenConfig(tc.p, tc.chartName, "/scratch/values.schema.json")

			values := make([]interface{}, len(cfg.Values))
			for i, v := range cfg.Values {
				values[i] = v
			}

			want := map[string]interface{}{
				"values":                 values,
				"draft":                  cfg.Draft,
				"indent":                 cfg.Indent,
				"output":                 NewCreateValuesSchemaInput(tc.p, tc.chartName).Path,
				"bundle":                 cfg.Bundle,
				"bundleRoot":             cfg.BundleRoot,
				"bundleWithoutID":        cfg.BundleWithoutID,
				"k8sSchemaURL":           cfg.K8sSchemaURL,
				"k8sSchemaVersion":       cfg.K8sSchemaVersion,
				"useHelmDocs":            cfg.UseHelmDocs,
				"noAdditionalProperties": cfg.NoAdditionalProperties,
				"noDefaultGlobal":        cfg.NoDefaultGlobal,
				"schemaRoot": map[string]interface{}{
					"additionalProperties": *cfg.SchemaRoot.AdditionalProperties,
				},
			}

			for key, got := range rendered {
				expected, ok := want[key]
				if !ok {
					t.Errorf("schema.yaml.template sets %q but newSchemaGenConfig does not; add it to the Go config and to this test", key)
					continue
				}
				if !reflect.DeepEqual(got, expected) {
					t.Errorf("%q drifted: schema.yaml.template has %#v, newSchemaGenConfig has %#v", key, got, expected)
				}
			}

			for key := range want {
				if _, ok := rendered[key]; !ok {
					t.Errorf("newSchemaGenConfig sets %q but schema.yaml.template does not", key)
				}
			}
		})
	}
}
