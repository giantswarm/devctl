package reposetup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// labelValue is a valid Kubernetes label value: empty, or alphanumeric at
// both ends with "-", "_" and "." between. Its length, at most 63, is
// checked apart.
var labelValue = regexp.MustCompile(`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`)

// chartLabelLine is the helm.sh/chart label in a rendered manifest.
var chartLabelLine = regexp.MustCompile(`(?m)^\s*helm\.sh/chart:\s*"?([^"\n]*)"?\s*$`)

// labelsProbe renders the chart template's common labels: the template's
// chart carries no resource template of its own.
const labelsProbe = `apiVersion: v1
kind: ConfigMap
metadata:
  name: probe
  labels:
    {{- include "labels.common" . | nindent 4 }}
`

// Test_Render_chartLabel renders the chart template's scaffold with helm for
// a release version and for long versions whose 63-character cut of
// "<name>-<version>" lands on ".", on the "_" a "+" becomes and on a run
// like "--." -- a branch build's <version>-dev.<branch>.<date>.<time>.<sha>,
// or the <version>+<digest> helm-controller installs -- and asserts that
// helm.sh/chart is a valid label value, which the API server otherwise
// refuses for every labelled object. The version is set by packaging, since
// `helm template --version` does not apply to a chart directory.
func Test_Render_chartLabel(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not installed; skipping the chart label render")
	}

	entry := declaration(t, "chart-app", "team-shield")
	dir := filepath.Join(t.TempDir(), entry.Name)
	if _, err := testRenderer().Render(context.Background(), RenderRequest{Team: "team-shield", Entry: entry, Dir: dir}); err != nil {
		t.Fatalf("render: %v", err)
	}
	chart := filepath.Join(dir, "helm", entry.Name)
	if err := os.WriteFile(filepath.Join(chart, "templates", "probe.yaml"), []byte(labelsProbe), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		version string
		want    string
	}{
		{version: "0.1.0", want: "example-0.1.0"},
		{
			version: "0.1.1-dev.renovate-helm-unittest-x.2026-09-22.14-54-24.h1a2b3c4",
			want:    "example-0.1.1-dev.renovate-helm-unittest-x.2026-09-22.14-54-24",
		},
		{
			version: "0.1.1-dev.renovate-helm-unittest-x.2026-09-22.14-54-24+h1a2b3c4",
			want:    "example-0.1.1-dev.renovate-helm-unittest-x.2026-09-22.14-54-24",
		},
		{
			version: "0.1.1-dev.renovate-helm-unittest-x.2026-09-22.14-54---.h1a2b3c4",
			want:    "example-0.1.1-dev.renovate-helm-unittest-x.2026-09-22.14-54",
		},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			pkg := t.TempDir()
			if out, err := exec.Command(helm, "package", chart, "--version", tc.version, "-d", pkg).CombinedOutput(); err != nil { // #nosec G204 -- helm from exec.LookPath, fixed arguments, test-only
				t.Fatalf("helm package: %v\n%s", err, out)
			}
			out, err := exec.Command(helm, "template", "probe", filepath.Join(pkg, entry.Name+"-"+tc.version+".tgz")).CombinedOutput() // #nosec G204 -- helm from exec.LookPath, fixed arguments, test-only
			if err != nil {
				t.Fatalf("helm template: %v\n%s", err, out)
			}
			m := chartLabelLine.FindSubmatch(out)
			if m == nil {
				t.Fatalf("no helm.sh/chart label rendered:\n%s", out)
			}
			got := string(m[1])
			if len(got) > 63 || !labelValue.MatchString(got) {
				t.Errorf("helm.sh/chart %q is not a valid label value", got)
			}
			if got != tc.want {
				t.Errorf("helm.sh/chart = %q, want %q", got, tc.want)
			}
		})
	}
}
