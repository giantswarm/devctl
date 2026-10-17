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

// fakeHelmInstall stands in for helm with no plugin installed: `helm plugin
// install --help` documents --verify only when $HELM_MAJOR is 4, and an install
// call is appended to $HELM_CALLS (helm v4 refuses one without --verify=false).
const fakeHelmInstall = `#!/bin/sh
if [ "$1" = plugin ] && [ "$2" = list ]; then
	exit 0
fi
if [ "$1" = plugin ] && [ "$2" = install ] && [ "$3" = --help ]; then
	echo "Usage: helm plugin install [options] <path|url>"
	[ "$HELM_MAJOR" = 4 ] && echo "      --verify   verify the plugin before installing it (default true)"
	exit 0
fi
if [ "$1" = plugin ] && [ "$2" = install ]; then
	if [ "$HELM_MAJOR" = 4 ] && [ "$3" != --verify=false ]; then
		echo "Error: plugin source does not support verification. Use --verify=false to skip verification" >&2
		exit 1
	fi
fi
echo "$*" >> "$HELM_CALLS"
`

// TestInstallHelmUnittestAcrossHelmMajors runs the generated install-helm-unittest
// target against a helm v3 and a helm v4: only v4 gets --verify=false, and both
// install the pinned plugin.
func TestInstallHelmUnittestAcrossHelmMajors(t *testing.T) {
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

	const plugin = "https://github.com/helm-unittest/helm-unittest --version "
	tests := []struct {
		major string
		want  string
	}{
		{major: "3", want: "plugin install " + plugin},
		{major: "4", want: "plugin install --verify=false " + plugin},
	}
	for _, tc := range tests {
		t.Run("helm v"+tc.major, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, in.Path), rendered.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "bin", "helm"), []byte(fakeHelmInstall), 0o700); err != nil { //nolint:gosec // a script that must be executable
				t.Fatal(err)
			}

			calls := filepath.Join(dir, "helm-calls")
			cmd := exec.Command("make", "-f", "Makefile.gen.app.mk", "install-helm-unittest")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"HELM_CALLS="+calls,
				"HELM_MAJOR="+tc.major,
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make install-helm-unittest: %v\n%s", err, out)
			}

			got, err := os.ReadFile(filepath.Clean(calls))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(got), tc.want) {
				t.Errorf("helm calls = %q, want prefix %q", got, tc.want)
			}
		})
	}
}

// fakeHelmFlakyInstall stands in for helm whose plugin download fails with a
// transient network error until $HELM_FAILURES installs have failed; every
// install and uninstall call is appended to $HELM_CALLS.
const fakeHelmFlakyInstall = `#!/bin/sh
if [ "$1" = plugin ] && [ "$2" = list ]; then
	exit 0
fi
if [ "$1" = plugin ] && [ "$2" = install ] && [ "$3" = --help ]; then
	exit 0
fi
echo "$1 $2" >> "$HELM_CALLS"
if [ "$1" = plugin ] && [ "$2" = install ]; then
	failed=$(grep -c "plugin install" "$HELM_CALLS")
	if [ "$failed" -le "$HELM_FAILURES" ]; then
		echo "curl: (55) Recv failure: Connection reset by peer" >&2
		exit 1
	fi
fi
`

// TestInstallHelmUnittestRetriesTransientErrors runs the generated
// install-helm-unittest target against a helm whose plugin download fails: two
// failures are retried, cleaning up the partial install before each retry, and
// a download that keeps failing fails the target after the last attempt.
func TestInstallHelmUnittestRetriesTransientErrors(t *testing.T) {
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

	const (
		install   = "plugin install\n"
		uninstall = "plugin uninstall\n"
	)
	tests := []struct {
		name     string
		failures string
		wantErr  bool
		want     string
	}{
		{
			name:     "transient",
			failures: "2",
			want:     install + uninstall + install + uninstall + install,
		},
		{
			name:     "persistent",
			failures: "99",
			wantErr:  true,
			want:     install + uninstall + install + uninstall + install,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, in.Path), rendered.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "bin", "helm"), []byte(fakeHelmFlakyInstall), 0o700); err != nil { //nolint:gosec // a script that must be executable
				t.Fatal(err)
			}

			calls := filepath.Join(dir, "helm-calls")
			cmd := exec.Command("make", "-f", "Makefile.gen.app.mk", "install-helm-unittest", "HELM_UNITTEST_RETRY_DELAY=0")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"HELM_CALLS="+calls,
				"HELM_FAILURES="+tc.failures,
			)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantErr {
				t.Fatalf("make install-helm-unittest: err = %v, want error %v\n%s", err, tc.wantErr, out)
			}

			got, err := os.ReadFile(filepath.Clean(calls))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("helm calls = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeHelmDocs stands in for helm-docs: every call is appended to $HELM_DOCS_CALLS.
const fakeHelmDocs = `#!/bin/sh
echo "$*" >> "$HELM_DOCS_CALLS"
`

// TestApplicationDefaultsToSingleChart runs the generated helm-docs target,
// which needs APPLICATION, in repositories that set it nowhere: one chart under
// helm/ is the default, several or none stop check-env with a message naming
// them, and a value set in Makefile.custom.mk or on the command line wins.
func TestApplicationDefaultsToSingleChart(t *testing.T) {
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

	testCases := []struct {
		name      string
		charts    []string
		custom    string
		args      []string
		wantCalls string
		wantErr   string
	}{
		{
			name:      "one chart",
			charts:    []string{"agent"},
			wantCalls: "--sort-values-order=file -c helm/agent -g helm/agent\n",
		},
		{
			name:    "several charts",
			charts:  []string{"one", "two"},
			wantErr: "helm/ does not hold exactly one chart (found: one two)",
		},
		{
			name:    "no chart",
			wantErr: "helm/ does not hold exactly one chart (found: none)",
		},
		{
			name:      "set in Makefile.custom.mk",
			charts:    []string{"one", "two"},
			custom:    "APPLICATION := two\n",
			wantCalls: "--sort-values-order=file -c helm/two -g helm/two\n",
		},
		{
			name:      "set on the command line",
			charts:    []string{"agent"},
			args:      []string{"APPLICATION=other"},
			wantCalls: "--sort-values-order=file -c helm/other -g helm/other\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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
			write("Makefile", "include Makefile.*.mk\n", 0o644)
			write(in.Path, rendered.String(), 0o644)
			write("bin/helm-docs", fakeHelmDocs, 0o755)
			if tc.custom != "" {
				write("Makefile.custom.mk", tc.custom, 0o644)
			}
			for _, chart := range tc.charts {
				write("helm/"+chart+"/Chart.yaml", "name: "+chart+"\n", 0o644)
			}

			calls := filepath.Join(dir, "helm-docs-calls")
			cmd := exec.Command("make", append([]string{"helm-docs"}, tc.args...)...)
			cmd.Dir = dir
			env := []string{}
			for _, e := range os.Environ() {
				if !strings.HasPrefix(e, "APPLICATION=") {
					env = append(env, e)
				}
			}
			cmd.Env = append(env,
				"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"HELM_DOCS_CALLS="+calls,
			)
			out, err := cmd.CombinedOutput()

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("make helm-docs succeeded, want it to stop at check-env\n%s", out)
				}
				if !strings.Contains(string(out), tc.wantErr) {
					t.Errorf("output does not contain %q:\n%s", tc.wantErr, out)
				}
				if _, err := os.Stat(calls); err == nil {
					t.Errorf("helm-docs ran although check-env failed")
				}
				return
			}
			if err != nil {
				t.Fatalf("make helm-docs: %v\n%s", err, out)
			}
			got, err := os.ReadFile(filepath.Clean(calls))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.wantCalls {
				t.Errorf("helm-docs calls = %q, want %q", got, tc.wantCalls)
			}
		})
	}
}
