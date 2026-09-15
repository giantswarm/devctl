package file

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
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

// renderAction executes the workflow template the way pkg/gen/internal.Execute
// does, returning the bytes that would be written to disk.
func renderAction(t *testing.T, p params.Params) string {
	t.Helper()

	in := NewCreatePreCommitActionInput(p)
	tpl, err := template.New(in.Path).
		Delims(in.TemplateDelims.Left, in.TemplateDelims.Right).
		Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

// Test_PreCommitActionGoGenerate verifies the generate step is opt-in. The job
// installs no code generators, so a repository whose directives need one must
// not get the step by default.
func Test_PreCommitActionGoGenerate(t *testing.T) {
	const step = "run: go generate ./..."

	t.Run("rendered for a go repository that asks for it", func(t *testing.T) {
		got := renderAction(t, params.Params{Language: "go", GoGenerate: true})
		if !strings.Contains(got, step) {
			t.Errorf("rendered workflow does not run the generate step:\n%s", got)
		}
		// golangci-lint compiles the packages it analyses, so the step is only
		// useful ahead of the hooks.
		if strings.Index(got, step) > strings.Index(got, "Execute pre-commit hooks") {
			t.Error("the generate step must precede the hooks")
		}
	})

	t.Run("absent for a go repository by default", func(t *testing.T) {
		got := renderAction(t, params.Params{Language: "go"})
		if strings.Contains(got, step) {
			t.Errorf("rendered workflow runs the generate step without GoGenerate:\n%s", got)
		}
	})

	t.Run("absent for a non-go repository", func(t *testing.T) {
		got := renderAction(t, params.Params{Language: "generic", GoGenerate: true})
		if strings.Contains(got, step) {
			t.Errorf("rendered workflow runs the generate step outside a go repository:\n%s", got)
		}
	})
}
