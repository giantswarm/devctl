package file

import (
	"testing"

	"github.com/giantswarm/devctl/v7/pkg/gen/input/precommit/internal/params"
)

func Test_NewCreatePreCommitConfigInput(t *testing.T) {
	testCases := []struct {
		name         string
		p            params.Params
		expectedPath string
		checkData    func(t *testing.T, data map[string]interface{})
	}{
		{
			name: "case 1: go language with helmchart flavor",
			p: params.Params{
				Dir:        "",
				Language:   "go",
				Flavors:    []string{"helmchart"},
				RepoName:   "my-repo",
				HelmCharts: []string{"my-app"},
			},
			expectedPath: ".pre-commit-config.yaml",
			checkData: func(t *testing.T, data map[string]interface{}) {
				t.Helper()
				if data["Language"] != "go" {
					t.Errorf("Language: expected %q, got %v", "go", data["Language"])
				}
				if data["HasHelmchart"] != true {
					t.Errorf("HasHelmchart: expected true, got %v", data["HasHelmchart"])
				}
				charts, ok := data["HelmCharts"].([]string)
				if !ok || len(charts) != 1 || charts[0] != "my-app" {
					t.Errorf("HelmCharts: expected [my-app], got %v", data["HelmCharts"])
				}
			},
		},
		{
			name: "case 2: generic language, no flavors",
			p: params.Params{
				Dir:      "",
				Language: "generic",
				Flavors:  []string{},
				RepoName: "other-repo",
			},
			expectedPath: ".pre-commit-config.yaml",
			checkData: func(t *testing.T, data map[string]interface{}) {
				t.Helper()
				if data["Language"] != "generic" {
					t.Errorf("Language: expected %q, got %v", "generic", data["Language"])
				}
				if data["HasBash"] != false {
					t.Errorf("HasBash: expected false, got %v", data["HasBash"])
				}
				if data["HasMd"] != false {
					t.Errorf("HasMd: expected false, got %v", data["HasMd"])
				}
				if data["RepoName"] != "other-repo" {
					t.Errorf("RepoName: expected %q, got %v", "other-repo", data["RepoName"])
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewCreatePreCommitConfigInput(tc.p)

			if got.Path != tc.expectedPath {
				t.Errorf("path: expected %q, got %q", tc.expectedPath, got.Path)
			}

			data, ok := got.TemplateData.(map[string]interface{})
			if !ok {
				t.Fatal("TemplateData is not map[string]interface{}")
			}

			tc.checkData(t, data)
		})
	}
}
