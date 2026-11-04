package file

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"text/template"

	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/precommit/internal/params"
)

// Execute the rendered hook's actual program and arguments. The fixtures need no
// chart dependencies, schema downloads, helm plugin or installed devctl binary.
func Test_HelmDocsDescriptionHook(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	in := NewCreatePreCommitConfigInput(params.Params{Flavors: []string{"helmchart"}, HelmCharts: []string{"test-chart"}})
	var rendered bytes.Buffer
	if err := template.Must(template.New("").Parse(in.TemplateBody)).Execute(&rendered, in.TemplateData); err != nil {
		t.Fatal(err)
	}
	var config struct {
		Repos []struct {
			Hooks []struct {
				ID                     string   `json:"id"`
				Language               string   `json:"language"`
				Entry                  string   `json:"entry"`
				Args                   []string `json:"args"`
				Files                  string   `json:"files"`
				AdditionalDependencies []string `json:"additional_dependencies"`
			} `json:"hooks"`
		} `json:"repos"`
	}
	if err := yaml.Unmarshal(rendered.Bytes(), &config); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}

	for _, repo := range config.Repos {
		for _, hook := range repo.Hooks {
			if hook.ID != "helm-docs-description-test-chart" {
				continue
			}
			if hook.Language != "python" || hook.Entry != "python -c" || !slices.Equal(hook.AdditionalDependencies, []string{"PyYAML==6.0.3"}) {
				t.Fatalf("hook does not install its pinned interpreter dependency: %+v", hook)
			}
			files := regexp.MustCompile(hook.Files)
			for path, want := range map[string]bool{
				"helm/test-chart/values.yaml":                           true,
				"helm/other-chart/values.yaml":                          false,
				"helm/test-chart/charts/vendored/values.yaml":           false,
				"helm/test-chart/values.schema.json":                    false,
				"helm/test-chart/zz_generated.app-platform.values.yaml": false,
			} {
				if got := files.MatchString(path); got != want {
					t.Errorf("hook selection for %s: got %v, want %v", path, got, want)
				}
			}
			if err := exec.Command(python, "-c", "import yaml").Run(); err != nil { // #nosec G204 -- interpreter from exec.LookPath, fixed dependency probe, test-only
				t.Skip("PyYAML not available; the generated hook installs PyYAML==6.0.3")
			}

			testCases := []struct {
				name      string
				values    string
				wantLines []string
			}{
				{
					name:      "case 0: issue reproduction",
					values:    "# -- Storage class of the volume.\n# Empty means the cluster default -- never a literal \"--\".\nstorageClass: \"\"\n",
					wantLines: []string{":2:"},
				},
				{
					name:   "case 1: plain continuation and first-line dash pairs",
					values: "# -- Storage class -- of the volume.\n# Plain description second line.\nstorageClass: \"\"\n",
				},
				{
					name:   "case 2: recognized annotations",
					values: "# -- Storage class.\n# @default -- cluster -- default\n# @notationType -- string\n# @section -- storage\nstorageClass: \"\"\n",
				},
				{
					name:      "case 3: nested path description after annotations with CRLF",
					values:    "storage:\r\n  # storage.class -- (string) Storage class.\r\n  # @default -- cluster default\r\n  # Empty -- means default.\r\n  class: \"\"\r\n",
					wantLines: []string{":4:"},
				},
				{
					name:   "case 4: ordinary comments and values",
					values: "# Ordinary prose -- no description opener.\nvalue: \"--\"\n# More ordinary prose -- no opener.\nother: true\n",
				},
				{
					name:   "case 5: comment-looking data in block and quoted scalars",
					values: "literal: |\n  # -- Example.\n  # Empty -- means default.\nfolded: >-\n  # -- Example.\n  # Empty -- means default.\nquoted: \"example\n  # -- Example.\n  # Empty -- means default.\n  \"\n",
				},
				{
					name:      "case 6: all dropped lines including a second opener and unicode",
					values:    "# -- Größe.\n# Empty -- means default.\n# -- Also silently dropped.\n# Another -- dropped line.\nvalue: \"\"\n",
					wantLines: []string{":2:", ":3:", ":4:"},
				},
				{
					name:   "case 7: a blank line ends the description block",
					values: "# -- Description.\n\n# Ordinary prose -- unrelated.\nvalue: \"\"\n",
				},
				{
					name:      "case 8: real comment after scalar data",
					values:    "literal: |\n  # -- Example.\n  # Example -- data.\n# -- Real description.\n# Real -- continuation.\nvalue: true\n",
					wantLines: []string{":5:"},
				},
			}
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "values.yaml")
					if err := os.WriteFile(path, []byte(tc.values), 0600); err != nil {
						t.Fatal(err)
					}
					args := append([]string{"-c"}, hook.Args...)
					cmd := exec.Command(python, append(args, path)...) // #nosec G204 -- interpreter from exec.LookPath, repository-rendered script, t.TempDir() fixture, test-only
					out, err := cmd.CombinedOutput()
					if len(tc.wantLines) == 0 {
						if err != nil || len(out) != 0 {
							t.Fatalf("valid input rejected: %v\n%s", err, out)
						}
					} else {
						if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 1 {
							t.Fatalf("expected exit 1, got %v\n%s", err, out)
						}
						for _, line := range tc.wantLines {
							if !strings.Contains(string(out), path+line) || !strings.Contains(string(out), "reword this line") {
								t.Errorf("missing file, line or remedy for %s: %s", line, out)
							}
						}
						if got := strings.Count(string(out), "silently dropped"); got != len(tc.wantLines) {
							t.Errorf("got %d diagnostics, want %d: %s", got, len(tc.wantLines), out)
						}
					}
					if got, err := os.ReadFile(path); err != nil || string(got) != tc.values { // #nosec G304 -- t.TempDir() fixture path, test-only
						t.Fatalf("check modified the input: %v", err)
					}
				})
			}
			return
		}
	}
	t.Fatal("rendered config has no helm-docs description check")
}
