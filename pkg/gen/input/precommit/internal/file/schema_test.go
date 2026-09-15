package file

import (
	"bytes"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
	"github.com/giantswarm/devctl/v8/pkg/gen/internal"
)

func Test_NewCreateSchemaYamlInput(t *testing.T) {
	testCases := []struct {
		name          string
		p             params.Params
		chartName     string
		expectedPath  string
		expectChartIn string
	}{
		{
			name:          "case 1: basic chart",
			p:             params.Params{Dir: "", K8sSchemaVersion: "v1.33.1"},
			chartName:     "my-app",
			expectedPath:  "helm/my-app/.schema.yaml",
			expectChartIn: "my-app",
		},
		{
			name:          "case 2: chart in subdirectory output",
			p:             params.Params{Dir: "", K8sSchemaVersion: "v1.29.0"},
			chartName:     "platform-chart",
			expectedPath:  "helm/platform-chart/.schema.yaml",
			expectChartIn: "platform-chart",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewCreateSchemaYamlInput(tc.p, tc.chartName)

			if got.Path != tc.expectedPath {
				t.Errorf("path: expected %q, got %q", tc.expectedPath, got.Path)
			}

			data, ok := got.TemplateData.(map[string]interface{})
			if !ok {
				t.Fatal("TemplateData is not map[string]interface{}")
			}

			chartName, ok := data["ChartName"].(string)
			if !ok || chartName != tc.expectChartIn {
				t.Errorf("ChartName: expected %q, got %v", tc.expectChartIn, data["ChartName"])
			}

			if _, ok := data["K8sSchemaVersion"]; !ok {
				t.Error("K8sSchemaVersion missing from TemplateData")
			}

			if _, ok := data["Language"]; ok {
				t.Error("Language should not be in schema TemplateData")
			}

			if !strings.Contains(got.TemplateBody, "{{ .ChartName }}") {
				t.Error("template body should reference .ChartName")
			}

			// k8sSchemaURL and k8sSchemaVersion must name the same version. The
			// read-only pre-commit hook runs helm-values-schema-json against this
			// file, and generateValuesSchema builds the same config in Go, so a
			// version that appears in one field and not the other splits the
			// pipeline devctl and the hook are supposed to share.
			var rendered bytes.Buffer
			if err := internal.Execute(t.Context(), &rendered, got); err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, line := range []string{
				"master/" + tc.p.K8sSchemaVersion + "/",
				`k8sSchemaVersion: "` + tc.p.K8sSchemaVersion + `"`,
			} {
				if !strings.Contains(rendered.String(), line) {
					t.Errorf("rendered .schema.yaml does not contain %q:\n%s", line, rendered.String())
				}
			}
		})
	}
}
