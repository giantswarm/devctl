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

// Test_HelmSchemaFixHook verifies the whole schema pipeline is emitted as a SINGLE local
// hook per chart: `helm schema` generate -> $ref fix (additionalProperties:false ->
// unevaluatedProperties:false, losisin/helm-values-schema-json#317) -> `schemalint
// normalize`.
//
// It must be one hook. pre-commit runs each hook independently against the file on disk
// and fails the run if any hook modified it, so every fixer must agree on one resting
// format. As separate hooks these three have rival resting formats (the generator's key
// ordering vs schemalint's canonical ordering) and `pre-commit run -a` can never converge
// (giantswarm/giantswarm#37267). Chaining them with `schemalint normalize` LAST makes the
// normalized form the single resting format, so the assertions below pin both the
// single-hook shape and the step order.
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
		"schemalint normalize helm/test-chart/values.schema.json",
		// schemalint is installed by the hook itself, so no new tooling is required.
		"additional_dependencies: ['github.com/giantswarm/schemalint/v2@v2.6.3']",
		"language: golang",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in rendered config, got:\n%s", want, got)
		}
	}

	// No step of the pipeline may be a separate hook: not the external generator hook,
	// not the old standalone $ref-fix hook, and not a standalone schemalint-normalize.
	// Any of those reintroduces rival resting formats (see the function doc). The
	// generator's `repo:`+`rev:` line pair is matched rather than the bare URL, because
	// the explanatory comment legitimately mentions the URL in prose.
	for _, unwanted := range []string{
		"helm-values-schema-json\n    rev:",
		"id: fix-schema-ref-unevaluated-test-chart",
		"id: schemalint-normalize",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("did not expect %q in rendered config (the whole schema pipeline must be one local hook), got:\n%s", unwanted, got)
		}
	}

	// Step order INSIDE the single hook's command: generate -> $ref fix -> normalize.
	// `schemalint normalize` must be the LAST writer, otherwise the resting format is not
	// the normalized one and the run never converges. Asserted on the command line itself,
	// not the whole config, since the explanatory comment mentions the same tool names.
	var pipeline string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "helm schema --config") {
			pipeline = line
			break
		}
	}
	if pipeline == "" {
		t.Fatalf("no pipeline command line found in rendered config:\n%s", got)
	}
	genIdx := strings.Index(pipeline, "helm schema --config")
	fixIdx := strings.Index(pipeline, "unevaluatedProperties")
	normIdx := strings.Index(pipeline, "schemalint normalize")
	if !(genIdx >= 0 && genIdx < fixIdx && fixIdx < normIdx) {
		t.Errorf("pipeline order must be generate -> $ref fix -> normalize (normalize last); "+
			"got positions generate=%d, fix=%d, normalize=%d in:\n%s", genIdx, fixIdx, normIdx, pipeline)
	}

	// Read-only verify still runs, and only after the pipeline produced the resting format.
	hookIdx := strings.Index(got, "id: helm-schema-test-chart")
	verifyIdx := strings.Index(got, "id: schemalint-verify")
	if !(hookIdx >= 0 && hookIdx < verifyIdx) {
		t.Errorf("schemalint-verify must run after the pipeline hook; got pipeline=%d, verify=%d in:\n%s",
			hookIdx, verifyIdx, got)
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
