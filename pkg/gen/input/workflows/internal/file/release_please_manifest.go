package file

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/gen/input"
)

//go:embed release_please_manifest.json.template
var releasePleaseManifestTemplate string

// semverTagRegexp matches the GiantSwarm "v<major>.<minor>.<patch>" tag
// convention used by the legacy create_release flow. Pre-release suffixes are
// excluded on purpose: they should not become the manifest baseline.
var semverTagRegexp = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

// originURLRegexp extracts owner/repo from common GitHub remote URL shapes
// (ssh, https, with or without the trailing .git suffix).
var originURLRegexp = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/.]+?)(?:\.git)?/?$`)

// githubAPIBaseURL is the GitHub REST API root. Overridden by tests to point
// at a local httptest server.
var githubAPIBaseURL = "https://api.github.com"

// latestReleasedVersion returns the highest "v<major>.<minor>.<patch>" tag
// (with the leading "v" stripped) reachable from the working directory, or ""
// if no such tag exists. Local git tags are checked first; if none are found,
// falls back to the GitHub API so shallow clones (e.g. `git clone --depth=1`
// in CI like giantswarm/github's align-files action, which doesn't fetch tags)
// still seed the manifest correctly.
func latestReleasedVersion() string {
	if v := latestReleasedVersionFromGit(); v != "" {
		return v
	}
	return latestReleasedVersionFromAPI()
}

func latestReleasedVersionFromGit() string {
	// --sort=-v:refname orders tags by version descending, so the first match
	// is the highest.
	cmd := exec.Command("git", "tag", "--list", "v*.*.*", "--sort=-v:refname")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if m := semverTagRegexp.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	return ""
}

// latestReleasedVersionFromAPI queries the GitHub REST API for the highest
// v<major>.<minor>.<patch> tag on the repo whose `origin` remote points to
// github.com. Returns "" on any error — the caller treats that as "no
// baseline" and writes an empty manifest, matching the pre-fallback behavior.
func latestReleasedVersionFromAPI() string {
	owner, repo := repoFromOriginRemote()
	if owner == "" || repo == "" {
		return ""
	}

	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/repos/%s/%s/tags?per_page=100", githubAPIBaseURL, owner, repo), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// Both env vars are common: GS_GITHUB_TOKEN is set by the align-files
	// action; GH_TOKEN is what `gh` and most local setups export. Either
	// works; unauthenticated requests also work for public repos but are
	// rate-limited to 60/hour.
	for _, env := range []string{"GS_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if tok := os.Getenv(env); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
			break
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return ""
	}

	type parsed struct {
		raw        string
		mj, mn, pa int
	}
	var versions []parsed
	for _, t := range tags {
		m := semverTagRegexp.FindStringSubmatch(t.Name)
		if m == nil {
			continue
		}
		parts := strings.Split(m[1], ".")
		mj, _ := strconv.Atoi(parts[0])
		mn, _ := strconv.Atoi(parts[1])
		pa, _ := strconv.Atoi(parts[2])
		versions = append(versions, parsed{raw: m[1], mj: mj, mn: mn, pa: pa})
	}
	if len(versions) == 0 {
		return ""
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].mj != versions[j].mj {
			return versions[i].mj > versions[j].mj
		}
		if versions[i].mn != versions[j].mn {
			return versions[i].mn > versions[j].mn
		}
		return versions[i].pa > versions[j].pa
	})
	return versions[0].raw
}

func repoFromOriginRemote() (string, string) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	m := originURLRegexp.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// NewReleasePleaseManifestInput returns a scaffolding Input (generate-once) for
// .release-please-manifest.json in the repository root. Release Please updates
// this file on every run to track the current released version.
//
// For a green-field repo with no prior tags the manifest is initialized empty
// ({}); release-please will populate it on the first release. For a repo
// migrating from the legacy create_release flow that already has v*.*.* tags
// (root component), the manifest is seeded with {".": "<latest>"} — without
// this seed release-please emits
//
//	Found release tag with component '', but not configured in manifest
//
// and exits without opening a Release PR because it has no per-path baseline
// to compute "what changed since the last release" against.
//
// The latest tag is discovered from local git first, then via the GitHub API
// as a fallback — needed because the align-files action shallow-clones
// (--depth=1) without tags, so a purely-local lookup returns nothing and the
// repo ends up seeded with {} despite having released tags.
func NewReleasePleaseManifestInput() input.Input {
	body := releasePleaseManifestTemplate
	if v := latestReleasedVersion(); v != "" {
		body = fmt.Sprintf("{\n  \".\": \"%s\"\n}\n", v)
	}
	return input.Input{
		Path:         filepath.Join(".", ".release-please-manifest.json"),
		TemplateBody: body,
	}
}
