package file

import (
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

func Test_NewCreateHelmReadmeInput(t *testing.T) {
	testCases := []struct {
		name         string
		p            params.Params
		chartName    string
		expectedPath string
	}{
		{
			name:         "case 1: basic chart",
			p:            params.Params{Dir: ""},
			chartName:    "my-app",
			expectedPath: "helm/my-app/README.md.gotmpl",
		},
		{
			name:         "case 2: different chart name",
			p:            params.Params{Dir: ""},
			chartName:    "platform-chart",
			expectedPath: "helm/platform-chart/README.md.gotmpl",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewCreateHelmReadmeInput(tc.p, tc.chartName)

			if got.Path != tc.expectedPath {
				t.Errorf("path: expected %q, got %q", tc.expectedPath, got.Path)
			}

			// Delimiters must be set to avoid processing helm-docs {{ }} directives.
			if got.TemplateDelims == (input.InputTemplateDelims{}) {
				t.Error("TemplateDelims should be set to non-default values")
			}

			// Template body must contain helm-docs directives verbatim.
			if !strings.Contains(got.TemplateBody, `{{ template "chart.header" . }}`) {
				t.Error("template body should contain helm-docs chart.header directive")
			}
		})
	}
}
