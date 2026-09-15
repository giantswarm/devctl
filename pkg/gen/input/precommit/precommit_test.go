package precommit

import (
	"bytes"
	"fmt"
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
	if err := os.MkdirAll(chartDir, 0750); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: test-chart\n"), 0600); err != nil {
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
	if len(inputs) != 2 {
		t.Fatalf("expected 2 schema inputs, got %d", len(inputs))
	}
	if inputs[0].Path != "helm/test-chart/.schema.yaml" {
		t.Errorf("path: expected %q, got %q", "helm/test-chart/.schema.yaml", inputs[0].Path)
	}
	if inputs[1].Path != "helm/test-chart/zz_generated.app-platform.values.yaml" {
		t.Errorf("path: expected %q, got %q", "helm/test-chart/zz_generated.app-platform.values.yaml", inputs[1].Path)
	}
}

// Test_HelmSchemaFixHook verifies the generated helm-schema hook is READ-ONLY: `devctl gen
// precommit` now writes helm/<chart>/values.schema.json itself (see
// Test_NewCreateValuesSchemaInput and generateValuesSchema in the internal/file package),
// so the hook only reproduces the generate -> $ref fix (additionalProperties:false ->
// unevaluatedProperties:false, losisin/helm-values-schema-json#317) -> normalize pipeline
// to a scratch file and diffs it against the committed one. It must never write
// helm/<chart>/values.schema.json, and its failure message must point at `devctl gen
// precommit` as the fix. See giantswarm/devctl#2195.
func Test_HelmSchemaFixHook(t *testing.T) {
	dir := t.TempDir()

	chartDir := filepath.Join(dir, "helm", "test-chart")
	if err := os.MkdirAll(chartDir, 0750); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: test-chart\n"), 0600); err != nil {
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

	// Read from go.mod (via the same pinnedVersions devctl itself uses), not hardcoded, so
	// this assertion cannot silently go stale the next time Renovate bumps either module.
	hvsjVersion, schemalintVersion := pinnedVersions()
	if hvsjVersion == "" || schemalintVersion == "" {
		t.Fatalf("pinnedVersions() returned empty version(s): hvsj=%q schemalint=%q", hvsjVersion, schemalintVersion)
	}

	for _, want := range []string{
		"id: helm-schema-test-chart",
		"helm-values-schema-json --config helm/test-chart/.schema.yaml",
		"unevaluatedProperties",
		"helm-values-schema-json/issues/317",
		"schemalint normalize",
		// Both binaries are installed AND pinned by the hook itself, so no tooling comes
		// from the environment and dev machines run the same versions CI does. Versions
		// are stamped from devctl's own go.mod (pinnedVersions/pinnedVersionsFromGoMod),
		// not hardcoded in the template, so this must match go.mod exactly.
		fmt.Sprintf("additional_dependencies: ['github.com/giantswarm/schemalint/v2@%s', 'github.com/losisin/helm-values-schema-json/v2@%s']", schemalintVersion, hvsjVersion),
		"language: golang",
		// Read-only: generates to a scratch file, diffs against the committed one, and
		// never writes it back.
		"tmp=$(mktemp)",
		`diff -u helm/test-chart/values.schema.json "$tmp"`,
		// Failure must point at the fix.
		"devctl gen precommit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in rendered config, got:\n%s", want, got)
		}
	}

	// No step of the pipeline may be a separate hook: not the external generator hook,
	// not a standalone $ref-fix hook, and not a standalone schemalint-normalize. The
	// generator's `repo:`+`rev:` line pair is matched rather than the bare URL, because
	// the explanatory comment legitimately mentions the URL in prose.
	//
	// `helm plugin list` must not appear either: the generator used to be a bare
	// `helm schema` resolved from the developer's global helm plugin dir, guarded only by
	// an existence check that any (wrong) version passed. See giantswarm/devctl#2179.
	for _, unwanted := range []string{
		"helm-values-schema-json\n    rev:",
		"id: fix-schema-ref-unevaluated-test-chart",
		"id: schemalint-normalize",
		"helm plugin list",
		// The generator must write to a scratch path, never straight to the output
		// -o helm/<chart>/values.schema.json in place, e.g. as the old pipeline's
		// generate step did.
		`--config helm/test-chart/.schema.yaml && `,
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("did not expect %q in rendered config, got:\n%s", unwanted, got)
		}
	}

	// Step order INSIDE the hook's script: generate -> $ref fix -> normalize -> diff.
	// Asserted on the args block itself, not the whole config, since the explanatory
	// comment mentions the same tool names.
	argsStart := strings.Index(got, "entry: sh -c")
	if argsStart < 0 {
		t.Fatalf("no hook entry found in rendered config:\n%s", got)
	}
	script := got[argsStart:]
	genIdx := strings.Index(script, "helm-values-schema-json --config")
	fixIdx := strings.Index(script, "unevaluatedProperties")
	normIdx := strings.Index(script, "schemalint normalize")
	diffIdx := strings.Index(script, "diff -u")
	if genIdx < 0 || genIdx >= fixIdx || fixIdx >= normIdx || normIdx >= diffIdx {
		t.Errorf("script order must be generate -> $ref fix -> normalize -> diff; "+
			"got positions generate=%d, fix=%d, normalize=%d, diff=%d in:\n%s", genIdx, fixIdx, normIdx, diffIdx, script)
	}

	// Read-only verify still runs, after the check hook.
	hookIdx := strings.Index(got, "id: helm-schema-test-chart")
	verifyIdx := strings.Index(got, "id: schemalint-verify")
	if hookIdx < 0 || hookIdx >= verifyIdx {
		t.Errorf("schemalint-verify must run after the check hook; got check=%d, verify=%d in:\n%s",
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
