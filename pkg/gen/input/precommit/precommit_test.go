package precommit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

func renderConfig(t *testing.T, c Config) string {
	t.Helper()

	p, err := New(c)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}
	in := p.CreatePreCommitConfig()

	tpl, err := template.New("config").Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	return out.String()
}

// Test_NodeDevLintHook verifies the dev-only ci:lint hook is emitted at the
// pre-push stage (so the CI pre-commit job, which runs the pre-commit stage,
// skips it) for every node repo and never for other languages, and that pre-push
// is then added to default_install_hook_types so `pre-commit install` wires it
// up.
func Test_NodeDevLintHook(t *testing.T) {
	t.Run("omitted for non-node", func(t *testing.T) {
		got := renderConfig(t, Config{Language: "go", RepoName: "my-repo"})
		if strings.Contains(got, "pre-push") {
			t.Errorf("expected no pre-push hook for go, got:\n%s", got)
		}
		if strings.Contains(got, "id: ci-lint") {
			t.Errorf("expected no ci:lint hook for go, got:\n%s", got)
		}
	})

	t.Run("emitted for node", func(t *testing.T) {
		got := renderConfig(t, Config{Language: "node"})
		if !strings.Contains(got, "default_install_hook_types: [pre-commit, commit-msg, pre-push]") {
			t.Errorf("expected pre-push in default_install_hook_types, got:\n%s", got)
		}
		for _, want := range []string{
			"- repo: local",
			"id: ci-lint",
			"entry: npm run ci:lint", // no lockfile in test dir -> npm fallback
			"stages: [pre-push]",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in rendered config, got:\n%s", want, got)
			}
		}
	})
}

func Test_New_WithHelmchartFlavor(t *testing.T) {
	dir := t.TempDir()

	chartDir := filepath.Join(dir, "helm", "test-chart")
	if err := os.MkdirAll(chartDir, 0755); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: test-chart\n"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	p, err := New(Config{
		Language: "go",
		Flavors:  []string{"helmchart"},
		RepoName: "my-repo",
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	inputs := p.CreateSchemaYamlInputs()
	if len(inputs) != 1 {
		t.Fatalf("expected 1 schema input, got %d", len(inputs))
	}
	if inputs[0].Path != "helm/test-chart/.schema.yaml" {
		t.Errorf("path: expected %q, got %q", "helm/test-chart/.schema.yaml", inputs[0].Path)
	}
}

func Test_New_WithoutHelmchartFlavor(t *testing.T) {
	p, err := New(Config{
		Language: "go",
		Flavors:  []string{},
		RepoName: "my-repo",
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	inputs := p.CreateSchemaYamlInputs()
	if len(inputs) != 0 {
		t.Errorf("expected 0 schema inputs without helmchart flavor, got %d", len(inputs))
	}
}

func Test_CreatePreCommitConfig_Path(t *testing.T) {
	p, err := New(Config{
		Language: "go",
		Flavors:  []string{},
		RepoName: "my-repo",
	})
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	got := p.CreatePreCommitConfig()
	if got.Path != ".pre-commit-config.yaml" {
		t.Errorf("path: expected %q, got %q", ".pre-commit-config.yaml", got.Path)
	}
}
