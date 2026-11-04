package gen

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/internal"
)

// A source or module-proxy build may have no generated .sha files. Its
// fallback tree link must not replace a committed template link by itself.
func Test_Execute_TemplateProvenance(t *testing.T) {
	const templateURL = "https://github.com/giantswarm/devctl/blob/abc/pkg/template.yaml.template"
	const fallbackURL = "https://github.com/giantswarm/devctl/tree/main"
	header := func(url string) string { return internal.Header("#", url) + "\n" }
	const body = "name: pre-commit\n"
	testCases := []struct {
		name     string
		original string
		rendered string
		want     string
	}{
		{
			name:     "case 0: source build preserves committed provenance",
			original: header(templateURL) + body,
			rendered: header(fallbackURL) + body,
			want:     header(templateURL) + body,
		},
		{
			name:     "case 1: module version alone does not change output",
			original: header("https://github.com/giantswarm/devctl/tree/v8.1.0") + body,
			rendered: header("https://github.com/giantswarm/devctl/tree/v8.2.0") + body,
			want:     header("https://github.com/giantswarm/devctl/tree/v8.1.0") + body,
		},
		{
			name:     "case 2: changed template output is written",
			original: header(templateURL) + body,
			rendered: header(fallbackURL) + "name: changed\n",
			want:     header(fallbackURL) + "name: changed\n",
		},
		{
			name:     "case 3: known template commit still updates provenance",
			original: header(templateURL) + body,
			rendered: header("https://github.com/giantswarm/devctl/blob/def/pkg/template.yaml.template") + body,
			want:     header("https://github.com/giantswarm/devctl/blob/def/pkg/template.yaml.template") + body,
		},
		{
			name:     "case 4: links in the body are significant",
			original: header(templateURL) + "url: " + templateURL + "\n",
			rendered: header(fallbackURL) + "url: " + fallbackURL + "\n",
			want:     header(fallbackURL) + "url: " + fallbackURL + "\n",
		},
		{
			name:     "case 5: a file without the generated header is updated",
			original: body,
			rendered: header(fallbackURL) + body,
			want:     header(fallbackURL) + body,
		},
		{
			name:     "case 6: a new file gets the fallback header",
			rendered: header(fallbackURL) + body,
			want:     header(fallbackURL) + body,
		},
		{
			name:     "case 7: extra header lines are significant",
			original: header(templateURL)[:len(header(templateURL))-2] + "name: extra\n#\n" + body,
			rendered: header(fallbackURL) + body,
			want:     header(fallbackURL) + body,
		},
		{
			name:     "case 8: schema config prelude is preserved",
			original: "# .schema.yaml\n# yaml-language-server: $schema=schema.json\n\n" + header(templateURL) + body,
			rendered: "# .schema.yaml\n# yaml-language-server: $schema=schema.json\n\n" + header(fallbackURL) + body,
			want:     "# .schema.yaml\n# yaml-language-server: $schema=schema.json\n\n" + header(templateURL) + body,
		},
		{
			name:     "case 9: changed schema config prelude is written",
			original: "# yaml-language-server: $schema=old.json\n\n" + header(templateURL) + body,
			rendered: "# yaml-language-server: $schema=new.json\n\n" + header(fallbackURL) + body,
			want:     "# yaml-language-server: $schema=new.json\n\n" + header(fallbackURL) + body,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "zz_generated.pre-commit.yaml")
			modified := time.Unix(1000000000, 0)
			if tc.original != "" {
				if err := os.WriteFile(path, []byte(tc.original), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, modified, modified); err != nil {
					t.Fatal(err)
				}
			}
			in := input.Input{Path: path, TemplateBody: tc.rendered}
			if err := Execute(t.Context(), in); err != nil {
				t.Fatalf("generate: %v", err)
			}
			got, err := os.ReadFile(path) // #nosec G304 -- fixed test filename under t.TempDir()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("generated file = %q, want %q", got, tc.want)
			}
			if tc.want == tc.original {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !info.ModTime().Equal(modified) {
					t.Error("unchanged template output rewrote the existing file")
				}
			}
		})
	}
}
