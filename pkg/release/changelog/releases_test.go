package changelog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// serveComponent serves a component's CHANGELOG.md at /CHANGELOG.md (a 404
// when changelog is empty) and its GitHub Releases at the GitHub API path,
// two per page, and registers it under KnownComponents. It counts the API
// requests.
func serveComponent(t *testing.T, changelog string, releases []fakeRelease) (string, *int) {
	t.Helper()
	apiCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/CHANGELOG.md", func(w http.ResponseWriter, r *http.Request) {
		if changelog == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(changelog))
	})
	var srv *httptest.Server
	mux.HandleFunc("/api/repos/giantswarm/fixture/releases", func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization header = %q, want the token", got)
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			_, _ = fmt.Sscanf(p, "%d", &page)
		}
		from, to := (page-1)*2, page*2
		if to >= len(releases) {
			to = len(releases)
		} else {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/repos/giantswarm/fixture/releases?page=%d>; rel="next"`, srv.URL, page+1))
		}
		_ = json.NewEncoder(w).Encode(releases[from:to])
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	previousAPI := githubAPIURL
	githubAPIURL = srv.URL + "/api/"
	t.Cleanup(func() { githubAPIURL = previousAPI })

	name := "test-fixture-" + t.Name()
	KnownComponents[name] = ParseParams{
		Tag:       "https://github.com/giantswarm/fixture/releases/tag/v{{.Version}}",
		Changelog: srv.URL + "/CHANGELOG.md",
	}
	t.Cleanup(func() { delete(KnownComponents, name) })
	return name, &apiCalls
}

// autoReleaseReleases are the GitHub Releases of a repository on the
// auto-release flow, newest first as GitHub lists them, with git-cliff notes.
var autoReleaseReleases = []fakeRelease{
	{TagName: "v1.4.0", Body: "### Added\n\n- Too new\n"},
	{TagName: "v1.3.0", Body: "### Added\n\n- *(api)* [**breaking**] Drop the v1 API in [#12](https://github.com/giantswarm/fixture/pull/12) by [@someone](https://github.com/someone)\n- New widget in [#11](https://github.com/giantswarm/fixture/pull/11)\n\n### Changed\n\n- Bump the base image\n\n**Full Changelog**: https://github.com/giantswarm/fixture/compare/v1.2.1...v1.3.0\n"},
	{TagName: "v1.3.0-rc.1", Body: "### Added\n\n- Only in the candidate\n", Prerelease: true},
	{TagName: "v1.3.0-draft", Body: "### Added\n\n- Only in the draft\n", Draft: true},
	{TagName: "v1.2.1", Body: "### Fixed\n\n- Crash on start\n"},
	{TagName: "v1.2.0", Body: "### Fixed\n\n- Already released\n"},
}

// pointer is a CHANGELOG.md frozen as a pointer at the GitHub Releases.
const pointer = "# Changelog\n\nThe changes of each release are in its notes on GitHub Releases:\nhttps://github.com/giantswarm/fixture/releases\n"

func TestParseChangelog_GitHubReleases(t *testing.T) {
	for name, changelog := range map[string]string{
		"CHANGELOG.md is a pointer":             pointer,
		"no CHANGELOG.md at the tag":            "",
		"CHANGELOG.md stops before the version": "# Changelog\n\n## [1.2.0] - 2026-01-01\n\n### Fixed\n\n- Already released\n",
	} {
		t.Run(name, func(t *testing.T) {
			component, apiCalls := serveComponent(t, changelog, autoReleaseReleases)

			v, err := ParseChangelog("token", component, "1.3.0", "1.2.0")
			if err != nil {
				t.Fatalf("ParseChangelog: %v", err)
			}

			want := "#### :warning: Breaking Changes\n\n" +
				"- *(api)* [**breaking**] Drop the v1 API in [#12](https://github.com/giantswarm/fixture/pull/12) by [@someone](https://github.com/someone)\n\n" +
				"#### Added\n\n- New widget in [#11](https://github.com/giantswarm/fixture/pull/11)\n\n" +
				"#### Changed\n\n- Bump the base image\n\n" +
				"#### Fixed\n\n- Crash on start"
			if v.Content != want {
				t.Errorf("content:\n%s\nwant:\n%s", v.Content, want)
			}
			if v.Name != "v1.2.0...v1.3.0" || v.Link != "https://github.com/giantswarm/fixture/compare/v1.2.0...v1.3.0" {
				t.Errorf("name %q, link %q", v.Name, v.Link)
			}
			if *apiCalls != 3 {
				t.Errorf("API calls = %d, want all 3 pages read", *apiCalls)
			}
		})
	}
}

func TestParseChangelog_GitHubReleasesCandidate(t *testing.T) {
	component, _ := serveComponent(t, pointer, autoReleaseReleases)

	v, err := ParseChangelog("token", component, "1.3.0-rc.1", "1.2.1")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}
	if v.Content != "#### Added\n\n- Only in the candidate" {
		t.Errorf("content:\n%s", v.Content)
	}
}

func TestParseChangelog_GitHubReleasesMissing(t *testing.T) {
	component, _ := serveComponent(t, pointer, autoReleaseReleases)

	_, err := ParseChangelog("token", component, "1.5.0", "1.4.0")
	if err == nil || !strings.Contains(err.Error(), "not found in changelog nor in GitHub releases") {
		t.Fatalf("err = %v, want the version range not found", err)
	}
}

func TestParseChangelog_ChangelogSectionReadsNoReleases(t *testing.T) {
	component, apiCalls := serveComponent(t, "# Changelog\n\n## [1.3.0] - 2026-02-01\n\n### Added\n\n- From the file\n\n## [1.2.0] - 2026-01-01\n", autoReleaseReleases)

	v, err := ParseChangelog("token", component, "1.3.0", "1.2.0")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}
	if v.Content != "#### Added\n\n- From the file" {
		t.Errorf("content:\n%s", v.Content)
	}
	if *apiCalls != 0 {
		t.Errorf("API calls = %d, want none", *apiCalls)
	}
}

func TestParseChangelog_GitHubReleasesNewComponent(t *testing.T) {
	// A component new in a release has no previous version: the release
	// notes pass an empty endVersion, and the range is the version alone.
	component, _ := serveComponent(t, pointer, autoReleaseReleases)

	v, err := ParseChangelog("token", component, "1.2.1", "")
	if err != nil {
		t.Fatalf("ParseChangelog: %v", err)
	}
	if v.Content != "#### Fixed\n\n- Crash on start" {
		t.Errorf("content:\n%s", v.Content)
	}
	if v.Name != "v1.2.1" || v.Link != "https://github.com/giantswarm/fixture/releases/tag/v1.2.1" {
		t.Errorf("name %q, link %q", v.Name, v.Link)
	}
}

func TestParseChangelog_ChangelogUnreadable(t *testing.T) {
	// Only a missing CHANGELOG.md falls back to the GitHub Releases: a
	// rate limit or a server error is an error, not quietly other notes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/CHANGELOG.md" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	name := "test-fixture-" + t.Name()
	KnownComponents[name] = ParseParams{
		Tag:       "https://github.com/giantswarm/fixture/releases/tag/v{{.Version}}",
		Changelog: srv.URL + "/CHANGELOG.md",
	}
	t.Cleanup(func() { delete(KnownComponents, name) })

	_, err := ParseChangelog("token", name, "1.3.0", "1.2.0")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want the 429", err)
	}
}
