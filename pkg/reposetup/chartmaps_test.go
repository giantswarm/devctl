package reposetup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Exercise the real scaffold and schema generation with a template that has
// empty maps. No Kubernetes schema references or cluster access are needed.
func Test_Render_openValueMaps(t *testing.T) {
	templates := t.TempDir()
	if err := copyTree(filepath.Join("testdata", "templates"), templates); err != nil {
		t.Fatal(err)
	}
	values := filepath.Join(templates, "template-app", "helm", "{APP-NAME}", "values.yaml")
	data, err := os.ReadFile(values) // #nosec G304 -- fixed fixture path under t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte(`
metrics:
  serviceMonitor:
    labels: {} # Tenant labels
podAnnotations: {}
podLabels: {}
nodeSelector: {}
affinity: {}
flow: {labels: {}}
closed: {} # @schema additionalProperties: false
# @schema additionalProperties: false
headClosed: {}
typed: {} # @schema additionalProperties: {type: string}
constrained: {} # @schema maxProperties: 0
`)...)
	if err := os.WriteFile(values, data, 0o600); err != nil { // #nosec G703 -- fixed fixture path under t.TempDir()
		t.Fatal(err)
	}

	// Both paths to a chart: the chart template itself, and a chart added
	// beside the Go service template.
	for _, tc := range []struct{ fixture, team string }{
		{"chart-app", "team-shield"},
		{"go-app", "team-bumblebee"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			entry := declaration(t, tc.fixture, tc.team)
			dir := filepath.Join(t.TempDir(), entry.Name)
			renderer := Renderer{Templates: DirTemplates{Root: templates}}
			if _, err := renderer.Render(context.Background(), RenderRequest{Team: tc.team, Entry: entry, Dir: dir}); err != nil {
				t.Fatalf("render: %v", err)
			}
			chart := filepath.Join(dir, "helm", entry.Name)
			valuesPath := filepath.Join(chart, "values.yaml")
			before, err := os.ReadFile(valuesPath) // #nosec G304 -- scaffold path under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(before), "Tenant labels") {
				t.Fatal("template's label comment was lost")
			}
			if err := openChartValueMaps(dir); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(valuesPath) // #nosec G304 -- scaffold path under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("annotating maps twice changed values.yaml")
			}
			schemaData, err := os.ReadFile(filepath.Join(chart, "values.schema.json")) // #nosec G304 -- scaffold path under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schemaData)))
			if err != nil {
				t.Fatal(err)
			}
			compiler := jsonschema.NewCompiler()
			if err := compiler.AddResource("values.schema.json", doc); err != nil {
				t.Fatal(err)
			}
			schema, err := compiler.Compile("values.schema.json")
			if err != nil {
				t.Fatal(err)
			}
			for _, check := range []struct {
				name  string
				value string
				valid bool
			}{
				{"empty", `{}`, true},
				{"labels", `{"metrics":{"serviceMonitor":{"labels":{"observability.giantswarm.io/tenant":"example"}}}}`, true},
				{"annotations", `{"podAnnotations":{"example.com/key":"value"}}`, true},
				{"pod labels", `{"podLabels":{"example.com/key":"value"}}`, true},
				{"selector", `{"nodeSelector":{"kubernetes.io/os":"linux"}}`, true},
				{"affinity", `{"affinity":{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[]}}}}`, true},
				{"flow map", `{"flow":{"labels":{"key":"value"}}}`, true},
				{"wrong type", `{"podAnnotations":"value"}`, false},
				{"unknown root field", `{"unknown":true}`, false},
				{"unknown nested field", `{"metrics":{"serviceMonitor":{"unknown":true}}}`, false},
				{"explicitly closed", `{"closed":{"key":"value"}}`, false},
				{"head annotation", `{"headClosed":{"key":"value"}}`, false},
				{"typed map", `{"typed":{"key":"value"}}`, true},
				{"typed map wrong type", `{"typed":{"key":1}}`, false},
				{"other annotation", `{"constrained":{"key":"value"}}`, false},
			} {
				t.Run(check.name, func(t *testing.T) {
					value, err := jsonschema.UnmarshalJSON(strings.NewReader(check.value))
					if err != nil {
						t.Fatal(err)
					}
					if err := schema.Validate(value); (err == nil) != check.valid {
						t.Fatalf("valid = %t, want %t: %v", err == nil, check.valid, err)
					}
				})
			}

			t.Run("helm template", func(t *testing.T) {
				helm, err := exec.LookPath("helm")
				if err != nil {
					t.Skip("helm not installed; schema validation above still runs")
				}
				probe := `apiVersion: v1
kind: ConfigMap
metadata:
  name: probe
  labels:
    {{- toYaml .Values.metrics.serviceMonitor.labels | nindent 4 }}
`
				if err := os.WriteFile(filepath.Join(chart, "templates", "probe.yaml"), []byte(probe), 0o600); err != nil {
					t.Fatal(err)
				}
				out, err := exec.Command(helm, "template", "probe", chart, "--set-string", "metrics.serviceMonitor.labels.tenant=example").CombinedOutput() // #nosec G204 -- helm from exec.LookPath, fixed arguments, test-only
				if err != nil {
					t.Fatalf("helm template with populated label map: %v\n%s", err, out)
				}
				if !strings.Contains(string(out), "tenant: example") {
					t.Fatalf("populated label missing from rendered chart:\n%s", out)
				}
				out, err = exec.Command(helm, "template", "probe", chart, "--set-string", "closed.key=value").CombinedOutput() // #nosec G204 -- helm from exec.LookPath, fixed arguments, test-only
				if err == nil || !strings.Contains(string(out), "closed") {
					t.Fatalf("helm did not enforce the explicit map restriction: %v\n%s", err, out)
				}
			})
		})
	}
}
