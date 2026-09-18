package reposetup

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// update regenerates the golden scaffolds in testdata/golden instead of
// asserting against them: `go test ./pkg/reposetup/ -update` after a
// template or generator change.
var update = flag.Bool("update", false, "update golden files")

// renderCases are the kinds of the derivation (D2), one golden each, and
// the Go service with the app flavour, whose scaffold carries the chart
// template's chart beside the Go template. The Node kind is deferred with
// its template.
var renderCases = []struct {
	name     string
	team     string
	template Template
	chart    Template
	options  map[string]string
}{
	{name: "go-service", team: "team-bumblebee", template: TemplateGo},
	{name: "go-app", team: "team-bumblebee", template: TemplateGo, chart: TemplateChart},
	{name: "chart-app", team: "team-shield", template: TemplateChart},
	{name: "chart-app-vendir", team: "team-shield", template: TemplateChart, options: map[string]string{
		OptionSync:          SyncVendir,
		OptionPatch:         PatchScript,
		OptionUpstreamRepo:  "https://github.com/example-org/example-chart",
		OptionUpstreamChart: "charts/example",
	}},
	{name: "configuration", team: "team-honeybadger", template: TemplateMinimal},
	{name: "customer", team: "team-planeteers", template: TemplateMinimal},
	{name: "python", team: "team-bumblebee", template: TemplateMinimal},
	{name: "kyverno-policy", team: "team-shield", template: TemplateMinimal},
}

// headerURL is the provenance line of a generated file: the URL of the
// devctl commit that last touched the template, which differs between a
// checkout and the release build, so the goldens carry a stable stand-in.
var headerURL = regexp.MustCompile(`https://github\.com/giantswarm/devctl/blob/[0-9a-f]+/`)

const fixedHeaderURL = "https://github.com/giantswarm/devctl/blob/<commit>/"

func testRenderer() Renderer {
	return Renderer{Templates: DirTemplates{Root: filepath.Join("testdata", "templates")}}
}

// declaration is the fixture team file's entry, validated and rendered as
// the dry run renders it.
func declaration(t *testing.T, fixture, team string) Entry {
	t.Helper()

	tf, err := ReadTeamFile(filepath.Join("testdata", "render", fixture+".yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tf.Team = team

	schema, err := EmbeddedSchema()
	if err != nil {
		t.Fatalf("embedded schema: %v", err)
	}
	result, err := Validator{Schema: schema}.Validate(context.Background(), Request{TeamFile: tf})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}
	entry := result.Entries[0]
	if !entry.Accepted {
		t.Fatalf("fixture refused: %v", entry.Problems)
	}
	return entry
}

// Test_Render renders every kind of the derivation and pins the tree: the
// command lines that generated it and every file with its content.
func Test_Render(t *testing.T) {
	for _, tc := range renderCases {
		t.Run(tc.name, func(t *testing.T) {
			entry := declaration(t, strings.TrimSuffix(tc.name, "-vendir"), tc.team)
			if entry.Template != tc.template {
				t.Fatalf("template = %s, want %s", entry.Template, tc.template)
			}
			if entry.Chart != tc.chart {
				t.Fatalf("chart = %q, want %q", entry.Chart, tc.chart)
			}

			dir := filepath.Join(t.TempDir(), entry.Name)
			scaffold, err := testRenderer().Render(context.Background(), RenderRequest{
				Team:    tc.team,
				Entry:   entry,
				Dir:     dir,
				Options: tc.options,
			})
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			got := manifest(t, scaffold)
			assertGolden(t, filepath.Join("testdata", "golden", tc.name+".golden"), got)
		})
	}
}

// Test_Render_alignRunIsNoop is the acceptance criterion: the generated
// files are what align-files' devctl writes for the same declaration, so
// running the generators again over the scaffold -- the first align run
// after creation -- changes nothing.
func Test_Render_alignRunIsNoop(t *testing.T) {
	for _, tc := range renderCases {
		t.Run(tc.name, func(t *testing.T) {
			entry := declaration(t, strings.TrimSuffix(tc.name, "-vendir"), tc.team)
			dir := filepath.Join(t.TempDir(), entry.Name)
			scaffold, err := testRenderer().Render(context.Background(), RenderRequest{Team: tc.team, Entry: entry, Dir: dir, Options: tc.options})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			before := manifest(t, scaffold)

			tf, err := ParseTeamFile(tc.team, strings.NewReader(entry.Rendered))
			if err != nil {
				t.Fatalf("parse rendered entry: %v", err)
			}
			fields, err := tf.Entries[0].Fields()
			if err != nil {
				t.Fatalf("fields: %v", err)
			}
			_, helmErr := os.Stat(filepath.Join(dir, "helm"))
			commands := genCommands(fields, genContext{HasHelm: helmErr == nil})
			if err := runGen(context.Background(), dir, nil, commands); err != nil {
				t.Fatalf("second generator run: %v", err)
			}

			if after := manifest(t, scaffold); after != before {
				t.Errorf("the generators changed the scaffold on the second run:\n%s", firstDifference(before, after))
			}
		})
	}
}

// Test_Render_refusals covers the request checks.
func Test_Render_refusals(t *testing.T) {
	entry := declaration(t, "chart-app", "team-shield")
	ctx := context.Background()

	cases := map[string]RenderRequest{
		"unknown option":        {Team: "team-shield", Entry: entry, Dir: t.TempDir(), Options: map[string]string{"colour": "blue"}},
		"value outside option":  {Team: "team-shield", Entry: entry, Dir: t.TempDir(), Options: map[string]string{OptionSync: "rsync"}},
		"vendir needs upstream": {Team: "team-shield", Entry: entry, Dir: t.TempDir(), Options: map[string]string{OptionSync: SyncVendir}},
		"script needs vendir":   {Team: "team-shield", Entry: entry, Dir: t.TempDir(), Options: map[string]string{OptionPatch: PatchScript}},
		"no team":               {Entry: entry, Dir: t.TempDir()},
		"refused entry":         {Team: "team-shield", Entry: Entry{Name: "x", Problems: []Problem{{Field: "name", Message: "taken"}}}, Dir: t.TempDir()},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := testRenderer().Render(ctx, req)
			if !IsInvalidConfig(err) {
				t.Fatalf("got %v, want an invalid config error", err)
			}
		})
	}

	t.Run("directory not empty", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := testRenderer().Render(ctx, RenderRequest{Team: "team-shield", Entry: entry, Dir: dir})
		if !IsInvalidConfig(err) {
			t.Fatalf("got %v, want an invalid config error", err)
		}
	})
}

// Test_Entry_Options pins what the dry run offers per template.
func Test_Entry_Options(t *testing.T) {
	if got := declaration(t, "chart-app", "team-shield").Options; len(got) != len(chartOptions) {
		t.Errorf("chart-only entry offers %d options, want %d", len(got), len(chartOptions))
	}
	if got := declaration(t, "go-service", "team-bumblebee").Options; got != nil {
		t.Errorf("Go entry offers options %v, want none", got)
	}
}

// manifest renders a scaffold as one text: the command lines, then every
// file with its mode and content, generated headers normalized.
func manifest(t *testing.T, s *Scaffold) string {
	t.Helper()

	var b strings.Builder
	fmt.Fprintf(&b, "template: %s\n", s.Template)
	if s.Chart != "" {
		fmt.Fprintf(&b, "chart: %s\n", s.Chart)
	}
	for _, c := range s.Commands {
		fmt.Fprintf(&b, "command: %s\n", c)
	}
	files, err := listFiles(s.Dir)
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	for _, f := range files {
		p := filepath.Join(s.Dir, filepath.FromSlash(f))
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(p) // #nosec G304 -- the scaffold rendered into t.TempDir()
		if err != nil {
			t.Fatal(err)
		}
		mode := ""
		if info.Mode()&0o111 != 0 {
			mode = " (executable)"
		}
		fmt.Fprintf(&b, "\n--- %s%s\n%s", f, mode, headerURL.ReplaceAllString(string(data), fixedHeaderURL))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			b.WriteString("\n<no newline at end of file>\n")
		}
	}
	return b.String()
}

func assertGolden(t *testing.T, golden, got string) {
	t.Helper()

	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil { // #nosec G306 -- test fixture
			t.Fatalf("update golden %s: %v", golden, err)
		}
		return
	}

	want, err := os.ReadFile(golden) // #nosec G304 -- fixed in-package testdata path
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", golden, err)
	}
	if got != string(want) {
		t.Errorf("scaffold does not match %s (run with -update to regenerate)\n%s", golden, firstDifference(string(want), got))
	}
}

// firstDifference shows where two manifests diverge.
func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return fmt.Sprintf("line %d:\n--- want ---\n%s\n--- got ---\n%s", i+1, w, g)
		}
	}
	return "no difference"
}
