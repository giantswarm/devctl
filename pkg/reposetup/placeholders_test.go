package reposetup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteCommonFiles_README: the plans scaffold keeps a template README
// that writeCommonFiles is given already on disk (as replacePlaceholders
// leaves it), while the minimal and Go scaffolds -- which carry none --
// still get the generic stub. giantswarm/honeybadger-plan came out of the
// plans template with its real README replaced by the stub; this pins the
// fix.
func TestWriteCommonFiles_README(t *testing.T) {
	s := substitutions{Name: "example-plans", Team: "team-cabbage", Description: "Team plans."}

	t.Run("plans keeps the template README", func(t *testing.T) {
		dir := t.TempDir()
		shipped := "# example-plans\n\nThe plans repository of @giantswarm/team-cabbage.\n"
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(shipped), 0o600); err != nil {
			t.Fatal(err)
		}

		require.NoError(t, writeCommonFiles(dir, TemplatePlans, s))

		got, err := os.ReadFile(filepath.Join(dir, "README.md")) // #nosec G304 -- a fixed path under t.TempDir()
		require.NoError(t, err)
		require.Equal(t, shipped, string(got), "the plans template's own README must survive writeCommonFiles")
	})

	t.Run("chart keeps the template README", func(t *testing.T) {
		dir := t.TempDir()
		shipped := "# example-chart\n\nA Helm chart.\n"
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(shipped), 0o600); err != nil {
			t.Fatal(err)
		}

		require.NoError(t, writeCommonFiles(dir, TemplateChart, s))

		got, err := os.ReadFile(filepath.Join(dir, "README.md")) // #nosec G304 -- a fixed path under t.TempDir()
		require.NoError(t, err)
		require.Equal(t, shipped, string(got))
	})

	t.Run("go template gets the generic stub", func(t *testing.T) {
		dir := t.TempDir()
		templateOwn := "# giantswarm/template\n\nDescribes the template itself.\n"
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(templateOwn), 0o600); err != nil {
			t.Fatal(err)
		}

		require.NoError(t, writeCommonFiles(dir, TemplateGo, s))

		got, err := os.ReadFile(filepath.Join(dir, "README.md")) // #nosec G304 -- a fixed path under t.TempDir()
		require.NoError(t, err)
		require.Equal(t, readme(s), string(got), "the Go template's own README describes the template and must be replaced")
	})

	t.Run("minimal scaffold gets the generic stub", func(t *testing.T) {
		dir := t.TempDir()

		require.NoError(t, writeCommonFiles(dir, TemplateMinimal, s))

		got, err := os.ReadFile(filepath.Join(dir, "README.md")) // #nosec G304 -- a fixed path under t.TempDir()
		require.NoError(t, err)
		require.Equal(t, readme(s), string(got))
	})
}
