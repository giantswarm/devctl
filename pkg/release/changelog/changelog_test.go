package changelog

import (
	"strings"
	"testing"
)

// parseSection drives just the section-parsing loop with a synthetic CHANGELOG body.
func parseSection(t *testing.T, body, currentVersion, endVersion string) CategorizedChanges {
	t.Helper()

	lines := strings.Split(body, "\n")

	inSection := false
	var currentCategory string
	categorizedChanges := CategorizedChanges{}

	startHeading := "## [" + currentVersion + "]"
	stopHeading := "## [" + endVersion + "]"

	for _, line := range lines {
		originalLine := line
		line = strings.TrimSpace(line)

		if strings.Contains(line, startHeading) {
			inSection = true
			continue
		}
		if inSection && strings.Contains(line, stopHeading) {
			break
		}
		if inSection && strings.HasPrefix(line, "## [") {
			currentCategory = ""
			continue
		}

		if inSection {
			if matches := categoryRegex.FindStringSubmatch(line); len(matches) > 1 {
				currentCategory = matches[1]
				continue
			}

			trimmedLeft := strings.TrimLeft(originalLine, " \t")
			indent := len(originalLine) - len(trimmedLeft)
			if indent >= 2 && (strings.HasPrefix(trimmedLeft, "- ") || strings.HasPrefix(trimmedLeft, "* ")) {
				item := strings.TrimSpace(trimmedLeft[2:])
				subBullet := "\n  - " + item
				switch currentCategory {
				case "Breaking":
					if len(categorizedChanges.Breaking) > 0 && !strings.Contains(categorizedChanges.Breaking[len(categorizedChanges.Breaking)-1], subBullet) {
						categorizedChanges.Breaking[len(categorizedChanges.Breaking)-1] += subBullet
					}
				case "Added":
					if len(categorizedChanges.Added) > 0 && !strings.Contains(categorizedChanges.Added[len(categorizedChanges.Added)-1], subBullet) {
						categorizedChanges.Added[len(categorizedChanges.Added)-1] += subBullet
					}
				case "Changed":
					if len(categorizedChanges.Changed) > 0 && !strings.Contains(categorizedChanges.Changed[len(categorizedChanges.Changed)-1], subBullet) {
						categorizedChanges.Changed[len(categorizedChanges.Changed)-1] += subBullet
					}
				case "Deprecated":
					if len(categorizedChanges.Deprecated) > 0 && !strings.Contains(categorizedChanges.Deprecated[len(categorizedChanges.Deprecated)-1], subBullet) {
						categorizedChanges.Deprecated[len(categorizedChanges.Deprecated)-1] += subBullet
					}
				case "Removed":
					if len(categorizedChanges.Removed) > 0 && !strings.Contains(categorizedChanges.Removed[len(categorizedChanges.Removed)-1], subBullet) {
						categorizedChanges.Removed[len(categorizedChanges.Removed)-1] += subBullet
					}
				case "Fixed":
					if len(categorizedChanges.Fixed) > 0 && !strings.Contains(categorizedChanges.Fixed[len(categorizedChanges.Fixed)-1], subBullet) {
						categorizedChanges.Fixed[len(categorizedChanges.Fixed)-1] += subBullet
					}
				case "Security":
					if len(categorizedChanges.Security) > 0 && !strings.Contains(categorizedChanges.Security[len(categorizedChanges.Security)-1], subBullet) {
						categorizedChanges.Security[len(categorizedChanges.Security)-1] += subBullet
					}
				}
			} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				item := strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
				item = strings.TrimSpace(item)
				switch currentCategory {
				case "Breaking":
					categorizedChanges.Breaking = appendUnique(categorizedChanges.Breaking, item)
				case "Added":
					categorizedChanges.Added = appendUnique(categorizedChanges.Added, item)
				case "Changed":
					categorizedChanges.Changed = appendUnique(categorizedChanges.Changed, item)
				case "Deprecated":
					categorizedChanges.Deprecated = appendUnique(categorizedChanges.Deprecated, item)
				case "Removed":
					categorizedChanges.Removed = appendUnique(categorizedChanges.Removed, item)
				case "Fixed":
					categorizedChanges.Fixed = appendUnique(categorizedChanges.Fixed, item)
				case "Security":
					categorizedChanges.Security = appendUnique(categorizedChanges.Security, item)
				}
			}
		}
	}

	return categorizedChanges
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCategorizedChanges_Security(t *testing.T) {
	body := `# Changelog

## [1.2.0] - 2024-03-01

### Added

- Support for new widget API

### Security

- Upgraded golang.org/x/crypto to v0.20.0 to address CVE-2023-48795
- Patched SQL injection in user input handler (GHSA-xxxx-yyyy-zzzz)

## [1.1.0] - 2024-01-15

### Fixed

- Typo in error message

[1.2.0]: https://github.com/example/repo/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/example/repo/releases/tag/v1.1.0
`

	changes := parseSection(t, body, "1.2.0", "1.1.0")

	if !equalSlices(changes.Added, []string{"Support for new widget API"}) {
		t.Errorf("Added: got %v, want [Support for new widget API]", changes.Added)
	}
	wantSecurity := []string{
		"Upgraded golang.org/x/crypto to v0.20.0 to address CVE-2023-48795",
		"Patched SQL injection in user input handler (GHSA-xxxx-yyyy-zzzz)",
	}
	if !equalSlices(changes.Security, wantSecurity) {
		t.Errorf("Security: got %v, want %v", changes.Security, wantSecurity)
	}
	if len(changes.Deprecated) != 0 {
		t.Errorf("Deprecated: got %v, want empty", changes.Deprecated)
	}
	if len(changes.Fixed) != 0 {
		t.Errorf("Fixed: got %v, want empty", changes.Fixed)
	}
}

func TestCategorizedChanges_Deprecated(t *testing.T) {
	body := `# Changelog

## [2.0.0] - 2024-04-01

### Changed

- Updated internal scheduler

### Deprecated

- Old v1 API endpoints will be removed in v3.0.0
- Legacy configuration format deprecated in favour of YAML

### Removed

- Dropped support for Go 1.20

## [1.9.0] - 2024-02-01

[2.0.0]: https://github.com/example/repo/compare/v1.9.0...v2.0.0
`

	changes := parseSection(t, body, "2.0.0", "1.9.0")

	if !equalSlices(changes.Changed, []string{"Updated internal scheduler"}) {
		t.Errorf("Changed: got %v, want [Updated internal scheduler]", changes.Changed)
	}
	wantDeprecated := []string{
		"Old v1 API endpoints will be removed in v3.0.0",
		"Legacy configuration format deprecated in favour of YAML",
	}
	if !equalSlices(changes.Deprecated, wantDeprecated) {
		t.Errorf("Deprecated: got %v, want %v", changes.Deprecated, wantDeprecated)
	}
	if !equalSlices(changes.Removed, []string{"Dropped support for Go 1.20"}) {
		t.Errorf("Removed: got %v, want [Dropped support for Go 1.20]", changes.Removed)
	}
	if len(changes.Security) != 0 {
		t.Errorf("Security: got %v, want empty", changes.Security)
	}
}

func TestCategorizedChanges_AllSections(t *testing.T) {
	body := `# Changelog

## [3.0.0] - 2024-06-01

### Added

- New metrics endpoint

### Changed

- Restructured configuration layout

### Deprecated

- HTTP basic auth support deprecated

### Removed

- Dropped Python 3.8 support

### Fixed

- Race condition in token refresh

### Security

- Patched SSRF vulnerability (CVE-2024-12345)

## [2.0.0] - 2024-04-01

[3.0.0]: https://github.com/example/repo/compare/v2.0.0...v3.0.0
`

	changes := parseSection(t, body, "3.0.0", "2.0.0")

	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"Added", changes.Added, []string{"New metrics endpoint"}},
		{"Changed", changes.Changed, []string{"Restructured configuration layout"}},
		{"Deprecated", changes.Deprecated, []string{"HTTP basic auth support deprecated"}},
		{"Removed", changes.Removed, []string{"Dropped Python 3.8 support"}},
		{"Fixed", changes.Fixed, []string{"Race condition in token refresh"}},
		{"Security", changes.Security, []string{"Patched SSRF vulnerability (CVE-2024-12345)"}},
	}
	for _, tc := range cases {
		if !equalSlices(tc.got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestOutputOrder(t *testing.T) {
	// Verify the rendered output follows KaC canonical order:
	// Breaking, Added, Changed, Deprecated, Removed, Fixed, Security
	body := `# Changelog

## [1.0.0] - 2024-06-01

### Security

- Patched CVE-2024-99999

### Fixed

- Fixed a crash on startup

### Deprecated

- OldFeature is deprecated

### Added

- Brand new feature

### Removed

- Removed legacy endpoint

### Changed

- Refactored internals

## [0.9.0] - 2024-05-01

[1.0.0]: https://github.com/example/repo/compare/v0.9.0...v1.0.0
`

	changes := parseSection(t, body, "1.0.0", "0.9.0")

	if len(changes.Added) == 0 {
		t.Fatal("Added should not be empty")
	}
	if len(changes.Changed) == 0 {
		t.Fatal("Changed should not be empty")
	}
	if len(changes.Deprecated) == 0 {
		t.Fatal("Deprecated should not be empty")
	}
	if len(changes.Removed) == 0 {
		t.Fatal("Removed should not be empty")
	}
	if len(changes.Fixed) == 0 {
		t.Fatal("Fixed should not be empty")
	}
	if len(changes.Security) == 0 {
		t.Fatal("Security should not be empty")
	}

	// Render output the same way ParseChangelog does and verify section order.
	var sb strings.Builder
	if len(changes.Added) > 0 {
		sb.WriteString("#### Added\n\n")
		for _, item := range changes.Added {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}
	if len(changes.Changed) > 0 {
		sb.WriteString("#### Changed\n\n")
		for _, item := range changes.Changed {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}
	if len(changes.Deprecated) > 0 {
		sb.WriteString("#### Deprecated\n\n")
		for _, item := range changes.Deprecated {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}
	if len(changes.Removed) > 0 {
		sb.WriteString("#### Removed\n\n")
		for _, item := range changes.Removed {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}
	if len(changes.Fixed) > 0 {
		sb.WriteString("#### Fixed\n\n")
		for _, item := range changes.Fixed {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}
	if len(changes.Security) > 0 {
		sb.WriteString("#### Security\n\n")
		for _, item := range changes.Security {
			sb.WriteString("- " + item + "\n")
		}
		sb.WriteString("\n")
	}

	output := sb.String()

	positions := []struct {
		name    string
		section string
	}{
		{"Added", "#### Added"},
		{"Changed", "#### Changed"},
		{"Deprecated", "#### Deprecated"},
		{"Removed", "#### Removed"},
		{"Fixed", "#### Fixed"},
		{"Security", "#### Security"},
	}

	pos := make(map[string]int, len(positions))
	for _, p := range positions {
		pos[p.name] = strings.Index(output, p.section)
		if pos[p.name] == -1 {
			t.Fatalf("section %q not found in output", p.section)
		}
	}

	order := []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}
	for i := 1; i < len(order); i++ {
		if pos[order[i-1]] >= pos[order[i]] {
			t.Errorf("section order violated: %s (pos %d) must come before %s (pos %d)",
				order[i-1], pos[order[i-1]], order[i], pos[order[i]])
		}
	}
}
