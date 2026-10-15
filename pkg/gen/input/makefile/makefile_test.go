package makefile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

// fakeHelm stands in for helm: `helm plugin list` reports the pinned
// helm-unittest, so install-helm-unittest installs nothing, and every other
// call is appended to $HELM_CALLS.
const fakeHelm = `#!/bin/sh
if [ "$1" = plugin ]; then
	echo "unittest 1.2.1"
	exit 0
fi
echo "$*" >> "$HELM_CALLS"
`

// TestHelmTestSkipsPlaceholderChart runs the generated helm-test target over a
// repository with a real chart and a template's placeholder chart: the real
// chart's suites run, the placeholder chart's do not.
func TestHelmTestSkipsPlaceholderChart(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}

	m, err := New(Config{Flavours: gen.FlavourSlice{gen.FlavourApp}})
	if err != nil {
		t.Fatal(err)
	}
	in := m.MakefileGenApp()
	tpl, err := template.New("makefile").Parse(in.TemplateBody)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	write := func(path, content string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(in.Path, rendered.String(), 0o644)
	write("bin/helm", fakeHelm, 0o755)
	for _, chart := range []string{"helm/real", "helm/{APP-NAME}"} {
		write(chart+"/Chart.yaml", "name: chart\n", 0o644)
		write(chart+"/tests/deployment_test.yaml", "suite: deployment\n", 0o644)
	}

	calls := filepath.Join(dir, "helm-calls")
	cmd := exec.Command("make", "-f", "Makefile.gen.app.mk", "helm-test")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HELM_CALLS="+calls,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make helm-test: %v\n%s", err, out)
	}

	got, err := os.ReadFile(filepath.Clean(calls))
	if err != nil {
		t.Fatal(err)
	}
	want := "unittest -f tests/*_test.yaml --with-subchart=false helm/real\n"
	if string(got) != want {
		t.Errorf("helm calls = %q, want %q", got, want)
	}
	if !strings.Contains(string(out), "skipping helm/{APP-NAME}") {
		t.Errorf("output does not name the skipped placeholder chart:\n%s", out)
	}
}
