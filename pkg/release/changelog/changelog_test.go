package changelog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// registerTestComponent adds a temporary entry to KnownComponents whose
// Changelog URL template points at the given httptest server, then returns a
// cleanup function that removes it.
func registerTestComponent(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	name := "test-fixture-" + t.Name()
	KnownComponents[name] = ParseParams{
		Tag:       srv.URL + "/releases/tag/v{{.Version}}",
		Changelog: srv.URL + "/CHANGELOG.md",
	}
	t.Cleanup(func() { delete(KnownComponents, name) })
	return name
}

func serveChangelog(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func TestParseChangelog_Security(t *testing.T) {
	srv := serveChangelog(`# Changelog

## [1.2.0] - 2024-03-01

### Added

- Support for new widget API

### Security

- Upgraded golang.org/x/crypto to v0.20.0 to address CVE-2023-48795
- Patched SQL injection in user input handler (GHSA-xxxx-yyyy-zzzz)

## [1.1.0] - 2024-01-15

### Fixed

- Typo in error message
`)
	defer srv.Close()

	component := registerTestComponent(t, srv)
	v, err := ParseChangelog(component, "1.2.0", "1.1.0")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}

	if !strings.Contains(v.Content, "#### Security") {
		t.Errorf("expected Security section in output, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "CVE-2023-48795") {
		t.Errorf("expected CVE entry in output, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "GHSA-xxxx-yyyy-zzzz") {
		t.Errorf("expected GHSA entry in output, got:\n%s", v.Content)
	}
	if strings.Contains(v.Content, "#### Deprecated") {
		t.Errorf("unexpected Deprecated section in output")
	}
}

func TestParseChangelog_Deprecated(t *testing.T) {
	srv := serveChangelog(`# Changelog

## [2.0.0] - 2024-04-01

### Changed

- Updated internal scheduler

### Deprecated

- Old v1 API endpoints will be removed in v3.0.0
- Legacy configuration format deprecated in favour of YAML

### Removed

- Dropped support for Go 1.20

## [1.9.0] - 2024-02-01
`)
	defer srv.Close()

	component := registerTestComponent(t, srv)
	v, err := ParseChangelog(component, "2.0.0", "1.9.0")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}

	if !strings.Contains(v.Content, "#### Deprecated") {
		t.Errorf("expected Deprecated section in output, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "Old v1 API endpoints") {
		t.Errorf("expected deprecation entry in output, got:\n%s", v.Content)
	}
	if strings.Contains(v.Content, "#### Security") {
		t.Errorf("unexpected Security section in output")
	}
}

func TestParseChangelog_AllSections(t *testing.T) {
	srv := serveChangelog(`# Changelog

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
`)
	defer srv.Close()

	component := registerTestComponent(t, srv)
	v, err := ParseChangelog(component, "3.0.0", "2.0.0")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}

	for _, want := range []string{
		"#### Added", "New metrics endpoint",
		"#### Changed", "Restructured configuration layout",
		"#### Deprecated", "HTTP basic auth support deprecated",
		"#### Removed", "Dropped Python 3.8 support",
		"#### Fixed", "Race condition in token refresh",
		"#### Security", "CVE-2024-12345",
	} {
		if !strings.Contains(v.Content, want) {
			t.Errorf("expected %q in output, got:\n%s", want, v.Content)
		}
	}
}

func TestParseChangelog_SectionOrder(t *testing.T) {
	// Sections appear in reverse-KaC order in the source; output must follow
	// KaC canonical order: Added, Changed, Deprecated, Removed, Fixed, Security.
	srv := serveChangelog(`# Changelog

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
`)
	defer srv.Close()

	component := registerTestComponent(t, srv)
	v, err := ParseChangelog(component, "1.0.0", "0.9.0")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}

	sections := []string{"#### Added", "#### Changed", "#### Deprecated", "#### Removed", "#### Fixed", "#### Security"}
	pos := make(map[string]int, len(sections))
	for _, s := range sections {
		p := strings.Index(v.Content, s)
		if p == -1 {
			t.Fatalf("section %q not found in output:\n%s", s, v.Content)
		}
		pos[s] = p
	}

	for i := 1; i < len(sections); i++ {
		if pos[sections[i-1]] >= pos[sections[i]] {
			t.Errorf("order violated: %s (pos %d) must precede %s (pos %d)",
				sections[i-1], pos[sections[i-1]], sections[i], pos[sections[i]])
		}
	}
}
