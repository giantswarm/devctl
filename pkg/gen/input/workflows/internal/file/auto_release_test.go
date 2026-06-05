package file

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows/internal/params"
)

// renderInput executes an input.Input the way pkg/gen writes it to disk and
// returns the rendered bytes.
func renderInput(t *testing.T, in input.Input) string {
	t.Helper()

	tpl := template.New("tpl")
	if in.TemplateDelims.Left != "" {
		tpl = tpl.Delims(in.TemplateDelims.Left, in.TemplateDelims.Right)
	}
	tpl, err := tpl.Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

// Test_NewAutoReleaseInput verifies the auto-release workflow lands as a
// regenerable file with the push-based git-cliff shape (push triggers on main +
// release-*, the git-cliff install, and the atomic gh release create step).
func Test_NewAutoReleaseInput(t *testing.T) {
	p := params.Params{Dir: ".github/workflows", RepoName: "mcp-kubernetes"}
	in := NewAutoReleaseInput(p)

	if want := ".github/workflows/zz_generated.auto-release.yaml"; in.Path != want {
		t.Errorf("path: expected %q, got %q", want, in.Path)
	}

	got := renderInput(t, in)
	for _, want := range []string{
		"name: Auto-release",
		"branches:",
		"- main",
		"- 'release-*'",
		"binary: git-cliff",
		"git-cliff --unreleased --bump --context",
		"gh release create",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("auto-release workflow missing %q:\n%s", want, got)
		}
	}
}

// Test_NewCliffInput verifies cliff.toml is generated at the repo root with the
// repo name templated into [remote.github] so git-cliff resolves PR links and
// authors against the consuming repo, and that bump rules are present.
func Test_NewCliffInput(t *testing.T) {
	in := NewCliffInput("mcp-kubernetes")

	if in.Path != "cliff.toml" {
		t.Errorf("path: expected %q, got %q", "cliff.toml", in.Path)
	}

	got := renderInput(t, in)
	for _, want := range []string{
		"owner = \"giantswarm\"",
		"repo = \"mcp-kubernetes\"",
		"features_always_bump_minor = true",
		"breaking_always_bump_major = true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cliff.toml missing %q:\n%s", want, got)
		}
	}

	// The git-cliff template expressions (single-brace) must survive the Go
	// template render untouched -- they are interpreted by git-cliff, not here.
	if !strings.Contains(got, "{{ remote.github.owner }}") {
		t.Errorf("cliff.toml should preserve git-cliff template expressions:\n%s", got)
	}
}
