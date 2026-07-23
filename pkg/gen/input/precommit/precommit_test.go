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

// Test_HelmSchemaFixHook verifies the combined schema hook. Schema generation
// (`helm schema`) and the post-processing fix ($ref + additionalProperties:false ->
// unevaluatedProperties:false, see losisin/helm-values-schema-json#317) are emitted as a
// SINGLE local hook per chart. They must be one hook: as two separate mutating hooks
// (generate, then fix) neither could ever pass `pre-commit run -a`, because helm-schema
// rewrites the committed fixed schema back to the buggy form every run and the fix
// rewrites it forward again. The combined hook must also run before schemalint-normalize.
func Test_HelmSchemaFixHook(t *testing.T) {
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

	got := renderConfig(t, Config{
		Language: "go",
		Flavors:  []string{"helmchart"},
		RepoName: "my-repo",
	})

	for _, want := range []string{
		"id: helm-schema-test-chart",
		"helm schema --config helm/test-chart/.schema.yaml",
		"unevaluatedProperties",
		"helm-values-schema-json/issues/317",
		"helm/test-chart/values.schema.json",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in rendered config, got:\n%s", want, got)
		}
	}

	// The generator and the fix must be a SINGLE local hook -- not the old separate
	// external helm-schema hook plus a fix hook, a layout that can never pass
	// `pre-commit run -a` (see the function doc). Match markers unique to the removed
	// external hook: its `repo:`+`rev:` line pair (the comment mentions the repo URL in
	// prose, but never followed by a `rev:` line) and the old separate fix-hook id.
	for _, unwanted := range []string{
		"helm-values-schema-json\n    rev:",
		"id: fix-schema-ref-unevaluated-test-chart",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("did not expect %q in rendered config (generation+fix must be one local hook), got:\n%s", unwanted, got)
		}
	}

	// The combined hook must run before schemalint re-normalizes the schema.
	schemaIdx := strings.Index(got, "id: helm-schema-test-chart")
	normalizeIdx := strings.Index(got, "id: schemalint-normalize")
	if !(schemaIdx >= 0 && schemaIdx < normalizeIdx) {
		t.Errorf("combined helm-schema hook must run before schemalint-normalize; "+
			"got positions helm-schema=%d, schemalint-normalize=%d in:\n%s",
			schemaIdx, normalizeIdx, got)
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
