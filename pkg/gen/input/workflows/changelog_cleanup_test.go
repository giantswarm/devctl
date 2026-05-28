package workflows

import (
	"os"
	"path/filepath"
	"testing"
)

func Test_RemoveChangelogUnreleasedSection(t *testing.T) {
	const preamble = `# Changelog

All notable changes to this project will be documented in this file.

`
	const v018 = `## [0.1.8] - 2026-05-28

### Added

- Something.
`
	const v017 = `## [0.1.7] - 2026-05-27

### Fixed

- Bug.
`

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unreleased with content is removed",
			input:    preamble + "## [Unreleased]\n\n### Added\n\n- New thing.\n\n" + v018 + "\n" + v017,
			expected: preamble + v018 + "\n" + v017,
		},
		{
			name:     "empty unreleased is removed",
			input:    preamble + "## [Unreleased]\n\n" + v018 + "\n" + v017,
			expected: preamble + v018 + "\n" + v017,
		},
		{
			name:     "no unreleased section is a no-op",
			input:    preamble + v018 + "\n" + v017,
			expected: preamble + v018 + "\n" + v017,
		},
		{
			name:     "linked unreleased form is recognised",
			input:    preamble + "## [Unreleased](https://example.com/compare/v0.1.8...HEAD)\n\n### Added\n\n- New.\n\n" + v018,
			expected: preamble + v018,
		},
		{
			// Degenerate: removing Unreleased leaves a trailing blank line in
			// the preamble that no longer separates anything. We trim it so
			// the file ends with a single POSIX-style terminating newline.
			name:     "unreleased as the only versioned section trims to preamble (no trailing blank)",
			input:    preamble + "## [Unreleased]\n\n### Added\n\n- Stub.\n",
			expected: "# Changelog\n\nAll notable changes to this project will be documented in this file.\n",
		},
		{
			name:     "preserves file with no trailing newline",
			input:    preamble + "## [Unreleased]\n\n- x\n\n## [0.1.0] - 2026-05-28\n\n- y",
			expected: preamble + "## [0.1.0] - 2026-05-28\n\n- y",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "CHANGELOG.md")
			if err := os.WriteFile(path, []byte(tc.input), 0644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			if err := RemoveChangelogUnreleasedSection(path); err != nil {
				t.Fatalf("RemoveChangelogUnreleasedSection: %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != tc.expected {
				t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", string(got), tc.expected)
			}
		})
	}
}

func Test_RemoveChangelogUnreleasedSection_MissingFile(t *testing.T) {
	// Calling on a non-existent path must be a clean no-op (no error), so that
	// repos without a CHANGELOG.md don't break a release-please gen run.
	if err := RemoveChangelogUnreleasedSection(filepath.Join(t.TempDir(), "does-not-exist.md")); err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
}
