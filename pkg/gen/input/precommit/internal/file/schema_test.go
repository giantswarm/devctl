package file

import (
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
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
		})
	}
}
