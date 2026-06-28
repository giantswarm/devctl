package ats

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// renderPipfile executes the Pipfile input the same way pkg/gen/internal.Execute
// does, returning the bytes that would be written to tests/ats/Pipfile.
func renderPipfile(t *testing.T) string {
	t.Helper()

	inputs := CreateATS()
	if len(inputs) != 1 {
		t.Fatalf("CreateATS() returned %d inputs, want 1", len(inputs))
	}
	in := inputs[0]

	if in.Path != "tests/ats/Pipfile" {
		t.Errorf("Pipfile Path = %q, want tests/ats/Pipfile", in.Path)
	}
	// A plain Pipfile is not a name pkg/gen treats as regenerable, so the input
	// must skip the regen check or an existing repo Pipfile would never be
	// overwritten by a central bump.
	if !in.SkipRegenCheck {
		t.Errorf("Pipfile input must set SkipRegenCheck so align overwrites the repo copy")
	}

	tpl, err := template.New("Pipfile").Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

// Test_PipfileRendersVerbatim verifies the embedded Pipfile passes through the
// template engine byte-identical (it has no template actions), so the file
// generated into each repo equals the canonical source Renovate bumps.
func Test_PipfileRendersVerbatim(t *testing.T) {
	got := renderPipfile(t)

	source := filepath.Join("internal", "file", "Pipfile")
	want, err := os.ReadFile(source) // #nosec G304 -- fixed in-package source path
	if err != nil {
		t.Fatalf("read embedded Pipfile source: %v", err)
	}

	if got != string(want) {
		t.Errorf("rendered Pipfile differs from embedded source (template corruption?)\n--- got ---\n%s\n--- source ---\n%s", got, string(want))
	}
}

// Test_PipfileCanonicalPins pins the exact == versions of the standard ATS
// stack. The pytest pin must stay below 9 to remain compatible with
// pytest-helm-charts (which requires pytest<9); a bare `pytest = "==9..."`
// would reintroduce the resolution conflict this centralization exists to
// prevent.
func Test_PipfileCanonicalPins(t *testing.T) {
	got := renderPipfile(t)

	for _, want := range []string{
		`pytest-helm-charts = "==1.3.4"`,
		`pytest = "==8.4.2"`,
		`pykube-ng = "==23.6.0"`,
		`pytest-rerunfailures = "==16.3"`,
		`requests = "==2.34.2"`,
		`[packages]`,
		`url = "https://pypi.org/simple"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("canonical Pipfile missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, `pytest = "==9`) {
		t.Errorf("pytest must stay <9 for pytest-helm-charts compatibility:\n%s", got)
	}
}
