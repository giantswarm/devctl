package renovate

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
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
