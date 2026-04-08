package precommit

import (
	"os"
	"path/filepath"
	"testing"
)

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
