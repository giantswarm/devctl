package ats

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

// renderVerbatim executes an input the same way pkg/gen/internal.Execute does,
// returning the bytes that would be written to disk.
func renderVerbatim(t *testing.T, in input.Input) string {
	t.Helper()

	tpl, err := template.New(in.Path).Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template %s: %v", in.Path, err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template %s: %v", in.Path, err)
	}

	return rendered.String()
}

// renderPipfile returns the bytes that would be written to tests/ats/Pipfile on
// the legacy (pipenv) layout.
func renderPipfile(t *testing.T) string {
	t.Helper()

	inputs := CreateATS(false)
	if len(inputs) != 1 {
		t.Fatalf("CreateATS(false) returned %d inputs, want 1", len(inputs))
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

	return renderVerbatim(t, in)
}

// readSource reads an embedded source file of this package.
func readSource(t *testing.T, name string) string {
	t.Helper()

	want, err := os.ReadFile(filepath.Join("internal", "file", name)) // #nosec G304 -- fixed in-package source path
	if err != nil {
		t.Fatalf("read embedded %s source: %v", name, err)
	}

	return string(want)
}

// Test_PipfileRendersVerbatim verifies the embedded Pipfile passes through the
// template engine byte-identical (it has no template actions), so the file
// generated into each repo equals the canonical source Renovate bumps.
func Test_PipfileRendersVerbatim(t *testing.T) {
	got := renderPipfile(t)

	if want := readSource(t, "Pipfile"); got != want {
		t.Errorf("rendered Pipfile differs from embedded source (template corruption?)\n--- got ---\n%s\n--- source ---\n%s", got, want)
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

// Test_UVLayoutInputs verifies the app-test-suite 1.x layout: pyproject.toml and
// uv.lock are emitted verbatim with the regen check skipped, and the pipenv
// files the generator used to emit are deleted.
func Test_UVLayoutInputs(t *testing.T) {
	inputs := CreateATS(true)
	if len(inputs) != 4 {
		t.Fatalf("CreateATS(true) returned %d inputs, want 4", len(inputs))
	}

	want := []struct {
		path   string
		delete bool
		source string
	}{
		{"tests/ats/pyproject.toml", false, "pyproject.toml"},
		{"tests/ats/uv.lock", false, "uv.lock"},
		{"tests/ats/Pipfile", true, ""},
		{"tests/ats/Pipfile.lock", true, ""},
	}
	for i, w := range want {
		in := inputs[i]
		if in.Path != w.path {
			t.Errorf("input %d Path = %q, want %q", i, in.Path, w.path)
		}
		if in.Delete != w.delete {
			t.Errorf("input %d (%s) Delete = %v, want %v", i, in.Path, in.Delete, w.delete)
		}
		if w.delete {
			continue
		}
		if !in.SkipRegenCheck {
			t.Errorf("%s input must set SkipRegenCheck so align overwrites the repo copy", in.Path)
		}
		if got, src := renderVerbatim(t, in), readSource(t, w.source); got != src {
			t.Errorf("rendered %s differs from embedded source (template corruption?)", in.Path)
		}
	}
}

// pinRegexps extract the canonical pins from both layouts: Pipfile
// `name = "==version"` and pyproject `"name==version"`.
var (
	pipfilePin   = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+) = "==([^"]+)"$`)
	pyprojectPin = regexp.MustCompile(`"([A-Za-z0-9_.-]+)==([^"]+)"`)
)

// Test_PyprojectMatchesPipfilePins verifies the two canonical layouts carry the
// same stack at the same versions. Renovate bumps them in one grouped PR
// (renovate-custom.json5, "ATS test dependencies"); this catches a bump that
// moved only one of them.
func Test_PyprojectMatchesPipfilePins(t *testing.T) {
	pipfile := map[string]string{}
	for _, m := range pipfilePin.FindAllStringSubmatch(readSource(t, "Pipfile"), -1) {
		pipfile[m[1]] = m[2]
	}
	pyproject := map[string]string{}
	for _, m := range pyprojectPin.FindAllStringSubmatch(readSource(t, "pyproject.toml"), -1) {
		pyproject[m[1]] = m[2]
	}

	if len(pipfile) == 0 || len(pyproject) == 0 {
		t.Fatalf("no pins parsed: Pipfile %d, pyproject %d", len(pipfile), len(pyproject))
	}
	for name, ver := range pipfile {
		if got, ok := pyproject[name]; !ok || got != ver {
			t.Errorf("pyproject.toml pins %s==%q, Pipfile pins %q", name, got, ver)
		}
	}
	for name := range pyproject {
		if _, ok := pipfile[name]; !ok {
			t.Errorf("pyproject.toml pins %s, which the Pipfile does not carry", name)
		}
	}

	// uv.lock must have been resolved from this pyproject: every pin appears as
	// a locked package version.
	lock := readSource(t, "uv.lock")
	for name, ver := range pyproject {
		want := "name = \"" + name + "\"\nversion = \"" + ver + "\""
		if !strings.Contains(lock, want) {
			t.Errorf("uv.lock does not lock %s %s (run `uv lock` in pkg/gen/input/ats/internal/file)", name, ver)
		}
	}
}
