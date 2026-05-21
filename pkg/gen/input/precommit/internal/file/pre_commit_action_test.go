package file

import (
	"testing"

	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

func Test_NewCreatePreCommitActionInput(t *testing.T) {
	testCases := []struct {
		name         string
		p            params.Params
		expectedPath string
	}{
		{
			name:         "case 1: default params",
			p:            params.Params{Dir: ""},
			expectedPath: ".github/workflows/zz_generated.pre-commit.yaml",
		},
		{
			name:         "case 2: go language",
			p:            params.Params{Dir: "", Language: "go"},
			expectedPath: ".github/workflows/zz_generated.pre-commit.yaml",
		},
		{
			name:         "case 3: helmchart flavor",
			p:            params.Params{Dir: "", Flavors: []string{"helmchart"}},
			expectedPath: ".github/workflows/zz_generated.pre-commit.yaml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewCreatePreCommitActionInput(tc.p)

			if got.Path != tc.expectedPath {
				t.Errorf("path: expected %q, got %q", tc.expectedPath, got.Path)
			}

			// Delimiters must be set to avoid processing GitHub Actions ${{ }} expressions.
			if got.TemplateDelims == (input.InputTemplateDelims{}) {
				t.Error("TemplateDelims should be set to non-default values")
			}

			if got.TemplateDelims.Left != "[[" || got.TemplateDelims.Right != "]]" {
				t.Errorf("TemplateDelims: expected [[/]], got %q/%q", got.TemplateDelims.Left, got.TemplateDelims.Right)
			}

			// Template data must contain required keys.
			data, ok := got.TemplateData.(map[string]any)
			if !ok {
				t.Fatal("TemplateData should be map[string]interface{}")
			}

			if _, exists := data["Language"]; !exists {
				t.Error("TemplateData should contain 'Language' key")
			}

			if _, exists := data["HasHelmchart"]; !exists {
				t.Error("TemplateData should contain 'HasHelmchart' key")
			}
		})
	}
}
