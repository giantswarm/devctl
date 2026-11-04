package file

import (
	"bytes"
	"reflect"
	"testing"
	"text/template"

	"sigs.k8s.io/yaml"

	"github.com/giantswarm/devctl/v8/pkg/gen/input/apptest/internal/params"
)

// Check the decoded values and their types, rather than a particular quoting
// style: flag strings must remain data when the framework loads this config.
func TestConfigLiteralValues(t *testing.T) {
	tests := []struct {
		name string
		p    params.Params
	}{
		{
			name: "empty flag defaults",
		},
		{
			name: "ordinary names",
			p: params.Params{
				AppName:  "fixture-app",
				RepoName: "fixture-app-repo",
				Catalog:  "default",
			},
		},
		{
			name: "YAML boolean number and null",
			p: params.Params{
				AppName:  "on",
				RepoName: "0123",
				Catalog:  "null",
			},
		},
		{
			name: "quotes escapes and config-looking data",
			p: params.Params{
				AppName:  "fixture\"\nproviders:\n- capz\n#",
				RepoName: "repo\\path\t: # literal",
				Catalog:  "{{ .appName }}\r\nappCatalog: other",
			},
		},
		{
			name: "Unicode and line separators",
			p: params.Params{
				AppName:  "café-雪",
				RepoName: "repo-🌲",
				Catalog:  "before\u2028middle\u2029after",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := NewCreateConfigInput(tt.p)
			tmpl, err := template.New("config").Parse(in.TemplateBody)
			if err != nil {
				t.Fatal(err)
			}
			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, in.TemplateData); err != nil {
				t.Fatal(err)
			}
			// The framework uses sigs.k8s.io/yaml. Decode without a typed
			// struct, which could coerce a YAML boolean/number back to a string.
			var got map[string]interface{}
			if err := yaml.UnmarshalStrict(rendered.Bytes(), &got); err != nil {
				t.Fatalf("decode generated config: %v", err)
			}
			want := map[string]interface{}{
				"appName":    tt.p.AppName,
				"repoName":   tt.p.RepoName,
				"appCatalog": tt.p.Catalog,
				"providers":  []interface{}{"capa"},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded config = %#v, want %#v", got, want)
			}
		})
	}
}
