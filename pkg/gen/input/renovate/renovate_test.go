package renovate

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/titanous/json5"
)

// update regenerates the golden fixtures in testdata/ instead of asserting
// against them. Run `go test ./pkg/gen/input/renovate/... -update` after
// changing the template.
var update = flag.Bool("update", false, "update golden files")

// render executes the renovate input the same way pkg/gen/internal.Execute
// does, returning the bytes that would be written to renovate.json5.
func render(t *testing.T, c Config) string {
	t.Helper()

	r, err := New(c)
	if err != nil {
		t.Fatalf("New() returned unexpected error: %v", err)
	}

	in := r.CreateRenovate()

	tpl := template.New("renovate")
	if in.TemplateDelims.Left != "" {
		tpl = tpl.Delims(in.TemplateDelims.Left, in.TemplateDelims.Right)
	}
	tpl, err = tpl.Parse(in.TemplateBody)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, in.TemplateData); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	return rendered.String()
}

// Test_ReviewersOmittedByDefault verifies that without reviewers the generated
// config has no `reviewers` key at all.
func Test_ReviewersOmittedByDefault(t *testing.T) {
	got := render(t, Config{Language: "go"})

	if strings.Contains(got, "reviewers") {
		t.Errorf("default renovate config should not contain a reviewers key:\n%s", got)
	}
	if strings.Contains(got, "assignAutomerge") {
		t.Errorf("default renovate config should not contain assignAutomerge without reviewers:\n%s", got)
	}
}

// Test_ReviewersRendered verifies that the reviewers flag is baked into the
// generated config as a valid, single-quoted top-level array, and survives a
// round-trip through a JSON5 parser.
func Test_ReviewersRendered(t *testing.T) {
	reviewers := []string{"team:team-rocket", "team:team-honeybadger"}
	got := render(t, Config{Language: "go", Reviewers: reviewers})

	for _, want := range []string{
		`reviewers: [`,
		`'team:team-rocket',`,
		`'team:team-honeybadger',`,
		`assignAutomerge: true,`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated config missing %q:\n%s", want, got)
		}
	}

	var parsed struct {
		Reviewers []string `json:"reviewers"`
	}
	if err := json5.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("generated config is not valid JSON5: %v\n%s", err, got)
	}
	if len(parsed.Reviewers) != len(reviewers) {
		t.Fatalf("parsed reviewers = %v, want %v", parsed.Reviewers, reviewers)
	}
	for i, r := range reviewers {
		if parsed.Reviewers[i] != r {
			t.Errorf("reviewer[%d] = %q, want %q", i, parsed.Reviewers[i], r)
		}
	}
}

// Test_FreeTextValuesEscaped verifies that the free-text --interval value is
// escaped into a valid single-quoted JSON5 string. Embedded quotes,
// backslashes and newlines must not break the generated file -- emitting the
// raw value verbatim would produce malformed JSON5.
func Test_FreeTextValuesEscaped(t *testing.T) {
	interval := `before 5am 'monday' \ "x"` + "\n!"
	got := render(t, Config{Language: "go", Interval: interval})

	var parsed struct {
		Schedule []string `json:"schedule"`
	}
	if err := json5.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("generated config is not valid JSON5 with a hostile interval: %v\n%s", err, got)
	}

	if len(parsed.Schedule) != 1 || parsed.Schedule[0] != interval {
		t.Errorf("schedule = %q, want exactly [%q]", parsed.Schedule, interval)
	}
}

// Test_CircleCIGeneratedOffOmitsPackageRules verifies the default: without the
// flag the generated Renovate config has no architect orb override.
func Test_CircleCIGeneratedOffOmitsPackageRules(t *testing.T) {
	got := render(t, Config{Language: "go"})

	if strings.Contains(got, "packageRules") {
		t.Errorf("default renovate config should not contain packageRules:\n%s", got)
	}
	if strings.Contains(got, "giantswarm/architect") {
		t.Errorf("default renovate config should not reference the architect orb:\n%s", got)
	}
}

// Test_CircleCIGeneratedOnDisablesArchitectOrb verifies that with the flag the
// generated Renovate config disables updates for the giantswarm/architect orb.
func Test_CircleCIGeneratedOnDisablesArchitectOrb(t *testing.T) {
	got := render(t, Config{Language: "go", CircleCIGenerated: true})

	for _, want := range []string{
		`packageRules: [`,
		`matchDatasources: [`,
		`'orb',`,
		`matchPackageNames: [`,
		`'giantswarm/architect',`,
		`enabled: false,`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("circleci-generated renovate config missing %q:\n%s", want, got)
		}
	}
}

// Test_CustomConfigOmittedByDefault verifies that without HasCustomConfig the
// generated config carries no renovate-custom.json5 extends entry, so repos
// without the file see no behavior change.
func Test_CustomConfigOmittedByDefault(t *testing.T) {
	got := render(t, Config{Language: "go", RepoName: "some-repo"})

	if strings.Contains(got, "renovate-custom.json5'") {
		t.Errorf("default renovate config should not extend renovate-custom.json5:\n%s", got)
	}

	var parsed struct {
		Extends []string `json:"extends"`
	}
	if err := json5.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("generated config is not valid JSON5: %v\n%s", err, got)
	}
}

// Test_CustomConfigExtendsLast verifies that with HasCustomConfig the
// repo-owned renovate-custom.json5 is referenced as the LAST extends entry,
// so its rules win over the shared presets per Renovate's merge order.
func Test_CustomConfigExtendsLast(t *testing.T) {
	got := render(t, Config{Language: "go", RepoName: "some-repo", HasCustomConfig: true})

	var parsed struct {
		Extends []string `json:"extends"`
	}
	if err := json5.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("generated config is not valid JSON5: %v\n%s", err, got)
	}

	if len(parsed.Extends) == 0 {
		t.Fatalf("generated config has no extends entries:\n%s", got)
	}

	want := "github>giantswarm/some-repo:renovate-custom.json5"
	last := parsed.Extends[len(parsed.Extends)-1]
	if last != want {
		t.Errorf("last extends entry = %q, want %q (full list: %v)", last, want, parsed.Extends)
	}

	for _, e := range parsed.Extends[:len(parsed.Extends)-1] {
		if strings.Contains(e, "renovate-custom.json5") {
			t.Errorf("renovate-custom.json5 must only appear as the last extends entry, found %q (full list: %v)", e, parsed.Extends)
		}
	}
}

// Test_Golden pins the exact rendered output -- quoting, trailing commas, and
// one-item-per-line layout -- for representative config combinations. Unlike
// the substring assertions above, it catches structural and formatting
// regressions. Regenerate with `-update` after intentional template changes.
func Test_Golden(t *testing.T) {
	cases := []struct {
		name   string
		config Config
	}{
		{
			// Bare Go repo: the most common generated config.
			name:   "default-go",
			config: Config{Language: "go"},
		},
		{
			// Python branch of the language switch.
			name:   "python",
			config: Config{Language: "python"},
		},
		{
			// Schedule branch isolated from the other optional blocks.
			name:   "schedule",
			config: Config{Language: "go", Interval: "before 5am on monday"},
		},
		{
			// Every optional block on at once, so the golden pins how they
			// compose and order.
			name: "full",
			config: Config{
				Language:          "go",
				Reviewers:         []string{"team:team-rocket", "team:team-honeybadger"},
				CircleCIGenerated: true,
				RepoName:          "some-repo",
				HasCustomConfig:   true,
				Interval:          "before 5am on monday",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, tc.config)

			// Every golden must itself be valid JSON5.
			var parsed map[string]interface{}
			if err := json5.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("rendered config is not valid JSON5: %v\n%s", err, got)
			}

			golden := filepath.Join("testdata", tc.name+".json5.golden")

			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("update golden %s: %v", golden, err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to create it)", golden, err)
			}

			if got != string(want) {
				t.Errorf("rendered config does not match %s (run with -update to regenerate)\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
			}
		})
	}
}
