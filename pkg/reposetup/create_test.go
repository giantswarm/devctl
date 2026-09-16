package reposetup

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestCreationDeclaration(t *testing.T) {
	d, err := Creation{
		Name:          "my-service",
		ComponentType: "service",
		Description:   "What it does",
		Visibility:    "public",
		Flavours:      []string{"app", "cli"},
		Language:      "go",
	}.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.YAML()
	if err != nil {
		t.Fatal(err)
	}
	want := `- name: my-service
  description: What it does
  visibility: public
  componentType: service
  gen:
    flavours:
      - app
      - cli
    language: go
    ci:
      generate: true
`
	if got != want {
		t.Errorf("rendered entry:\n%s\nwant:\n%s", got, want)
	}

	// Empty fields are left out, so the validation names them; without a
	// CircleCI job the pipeline is not generated.
	d, err = Creation{Name: "bare"}.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	got, _ = d.YAML()
	if want := "- name: bare\n  gen:\n    ci:\n      generate: false\n"; got != want {
		t.Errorf("bare entry:\n%s\nwant:\n%s", got, want)
	}

	// A configuration repository has no CircleCI job: generate is false.
	d, err = Creation{Name: "configs", ComponentType: "configuration", Flavours: []string{"generic"}, Language: "generic"}.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	got, _ = d.YAML()
	if want := "- name: configs\n  componentType: configuration\n  gen:\n    flavours:\n      - generic\n    language: generic\n    ci:\n      generate: false\n"; got != want {
		t.Errorf("configuration entry:\n%s\nwant:\n%s", got, want)
	}

	if _, err := (Creation{}).Declaration(); !IsInvalidConfig(err) {
		t.Errorf("no name: got %v, want invalidConfigError", err)
	}
}

func TestInsertEntry(t *testing.T) {
	const header = "# yaml-language-server: $schema=../.github/repositories.schema.json\n"
	entry := func(name string) string { return "- name: " + name + "\n  componentType: service\n" }
	newEntry := "- name: NEW\n  gen:\n    ci:\n      generate: false\n"
	d, err := Creation{Name: "NEW"}.Declaration()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		file string
		want string
	}{
		{
			name: "between, case-insensitively",
			file: header + entry("alpha") + entry("omega"),
			want: header + entry("alpha") + newEntry + entry("omega"),
		},
		{
			name: "first: the header stays on top",
			file: header + entry("omega"),
			want: header + newEntry + entry("omega"),
		},
		{
			name: "last: appended, a missing final newline added",
			file: strings.TrimSuffix(header+entry("alpha"), "\n"),
			want: header + entry("alpha") + newEntry,
		},
		{
			name: "a column-0 comment block belongs to the entry below it",
			file: header + entry("alpha") + "# omega is special\n# in two lines\n" + entry("omega"),
			want: header + entry("alpha") + newEntry + "# omega is special\n# in two lines\n" + entry("omega"),
		},
		{
			name: "an indented comment stays with the entry above it",
			file: header + entry("alpha") + "  # about alpha\n" + entry("omega"),
			want: header + entry("alpha") + "  # about alpha\n" + newEntry + entry("omega"),
		},
		{
			name: "no entries: after the header",
			file: header + "# nothing yet\n",
			want: header + "# nothing yet\n" + newEntry,
		},
		{
			name: "name not the first key",
			file: header + "- componentType: customer\n  name: alpha\n" + "- componentType: customer\n  name: omega\n",
			want: header + "- componentType: customer\n  name: alpha\n" + newEntry + "- componentType: customer\n  name: omega\n",
		},
		{
			name: "unsorted file: after the last name that sorts before",
			file: header + entry("zulu") + entry("alpha") + entry("omega"),
			want: header + entry("zulu") + entry("alpha") + newEntry + entry("omega"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InsertEntry("team-test", []byte(tc.file), d)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// The real Bumblebee file: the insertion keeps every other byte and the
// parsed file gains exactly the new entry at its place.
func TestInsertEntryRealFile(t *testing.T) {
	file, err := os.ReadFile("testdata/team-bumblebee.yaml")
	if err != nil {
		t.Fatal(err)
	}
	before, err := ParseTeamFile("team-bumblebee", bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	d, err := Creation{Name: "cluster-aaa", ComponentType: "service", Flavours: []string{"app"}, Language: "go"}.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	got, err := InsertEntry("team-bumblebee", file, d)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseTeamFile("team-bumblebee", bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != len(before.Entries)+1 {
		t.Fatalf("entries: got %d, want %d", len(after.Entries), len(before.Entries)+1)
	}
	rendered, _ := d.YAML()
	if !bytes.Contains(got, []byte(rendered)) || len(got) != len(file)+len(rendered) {
		t.Errorf("the file changed by more than the new entry: %d bytes, want %d", len(got), len(file)+len(rendered))
	}
	var names []string
	for _, e := range after.Entries {
		names = append(names, e.Name)
	}
	at := -1
	for i, n := range names {
		if n == "cluster-aaa" {
			at = i
		}
	}
	if at <= 0 || strings.ToLower(names[at-1]) >= "cluster-aaa" || (at+1 < len(names) && strings.ToLower(names[at+1]) < "cluster-aaa") {
		t.Errorf("cluster-aaa placed at %d between %q and %q", at, names[max(at-1, 0)], names[min(at+1, len(names)-1)])
	}
}
