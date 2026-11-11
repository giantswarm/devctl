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

// TestWriteCommonFiles_badges: a private repository's template README
// loses the CircleCI and Scorecard badges, which 404 for it, and nothing
// else; a public repository's keeps them.
func TestWriteCommonFiles_badges(t *testing.T) {
	const badges = "[![CircleCI](https://dl.circleci.com/status-badge/img/gh/giantswarm/example/tree/main.svg?style=svg)](https://dl.circleci.com/status-badge/redirect/gh/giantswarm/example/tree/main)\n" +
		"[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/giantswarm/example/badge)](https://securityscorecards.dev/viewer/?uri=github.com/giantswarm/example)\n\n"
	const body = "[Guide](https://handbook.giantswarm.io/)\n\n## Example\n\n[![Other](https://example.com/badge.svg)](https://example.com)\n"

	cases := map[string]struct {
		private bool
		want    string
	}{
		"private drops the badges": {private: true, want: body},
		"public keeps the badges":  {private: false, want: badges + body},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(badges+body), 0o600); err != nil {
				t.Fatal(err)
			}

			s := substitutions{Name: "example", Team: "team-shield", Private: tc.private}
			require.NoError(t, writeCommonFiles(dir, TemplateChart, s))

			got, err := os.ReadFile(filepath.Join(dir, "README.md")) // #nosec G304 -- a fixed path under t.TempDir()
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
		})
	}
}
