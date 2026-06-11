package renovate

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/titanous/json5"
)

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
}

// Test_ReviewersRendered verifies that the reviewers flag is baked into the
// generated config as a valid, double-quoted top-level array, and survives a
// round-trip through a JSON5 parser.
func Test_ReviewersRendered(t *testing.T) {
	reviewers := []string{"team:team-rocket", "team:team-honeybadger"}
	got := render(t, Config{Language: "go", Reviewers: reviewers})

	for _, want := range []string{
		`"reviewers": [`,
		`"team:team-rocket",`,
		`"team:team-honeybadger",`,
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
		`"packageRules"`,
		`"matchDatasources": ["orb"]`,
		`"matchPackageNames": ["giantswarm/architect"]`,
		`"enabled": false`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("circleci-generated renovate config missing %q:\n%s", want, got)
		}
	}
}
