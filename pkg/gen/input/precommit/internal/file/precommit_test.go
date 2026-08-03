package file

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// Test_RefFixPython runs the $ref fix step of the generated helm-schema pipeline hook --
// the actual bug fix -- against a schema shaped like `helm schema` output. Objects with a
// `$ref` must trade `additionalProperties: false` for `unevaluatedProperties: false`
// (losisin/helm-values-schema-json#317), and objects WITHOUT a `$ref` must be left alone:
// there `additionalProperties: false` is correct 2020-12 and rewriting it would silently
// widen the schema.
//
// The subprocess runs under `LC_ALL=C` with a non-ASCII description (helm-docs comments
// carry en dashes and typographic quotes routinely), which is exactly the case that fails
// with a UnicodeDecodeError if the program ever loses its explicit `encoding="utf-8"`.
func Test_RefFixPython(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	const in = `{
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "additionalProperties": false,
    "properties": {
        "affinity": {
            "$ref": "#/$defs/io.k8s.api.core.v1.Affinity",
            "additionalProperties": false,
            "description": "Pod affinity — “typographic” quotes"
        },
        "plain": {
            "additionalProperties": false,
            "properties": {"a": {"type": "string"}},
            "type": "object"
        }
    }
}`

	path := filepath.Join(t.TempDir(), "values.schema.json")
	if err := os.WriteFile(path, []byte(in), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	cmd := exec.Command(python, "-c", refFixPython, path)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LC_CTYPE=C", "PYTHONUTF8=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("$ref fix step failed: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got struct {
		AdditionalProperties  *bool `json:"additionalProperties"`
		UnevaluatedProperties *bool `json:"unevaluatedProperties"`
		Properties            struct {
			Affinity struct {
				Ref                   string `json:"$ref"`
				Description           string `json:"description"`
				AdditionalProperties  *bool  `json:"additionalProperties"`
				UnevaluatedProperties *bool  `json:"unevaluatedProperties"`
			} `json:"affinity"`
			Plain struct {
				AdditionalProperties  *bool `json:"additionalProperties"`
				UnevaluatedProperties *bool `json:"unevaluatedProperties"`
			} `json:"plain"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, raw)
	}

	// The $ref node: additionalProperties gone, unevaluatedProperties: false, $ref and the
	// non-ASCII description intact.
	a := got.Properties.Affinity
	if a.AdditionalProperties != nil {
		t.Errorf("affinity: additionalProperties should be dropped next to $ref, got %v", *a.AdditionalProperties)
	}
	if a.UnevaluatedProperties == nil || *a.UnevaluatedProperties {
		t.Errorf("affinity: expected unevaluatedProperties: false, got %v", a.UnevaluatedProperties)
	}
	if a.Ref != "#/$defs/io.k8s.api.core.v1.Affinity" {
		t.Errorf("affinity: $ref not preserved, got %q", a.Ref)
	}
	if a.Description != "Pod affinity — “typographic” quotes" {
		t.Errorf("affinity: non-ASCII description not preserved, got %q", a.Description)
	}

	// Nodes without a $ref keep their (correct) additionalProperties: false.
	for name, node := range map[string]struct {
		AdditionalProperties  *bool
		UnevaluatedProperties *bool
	}{
		"properties.plain": {got.Properties.Plain.AdditionalProperties, got.Properties.Plain.UnevaluatedProperties},
		"root":             {got.AdditionalProperties, got.UnevaluatedProperties},
	} {
		if node.AdditionalProperties == nil || *node.AdditionalProperties {
			t.Errorf("%s: expected additionalProperties: false to be kept, got %v", name, node.AdditionalProperties)
		}
		if node.UnevaluatedProperties != nil {
			t.Errorf("%s: unevaluatedProperties must not be added without a $ref, got %v", name, *node.UnevaluatedProperties)
		}
	}
}

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
