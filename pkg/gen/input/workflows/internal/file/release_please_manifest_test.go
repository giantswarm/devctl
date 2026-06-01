package file

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

func Test_repoFromOriginRemote(t *testing.T) {
	testCases := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
	}{
		{"ssh with .git", "git@github.com:giantswarm/mcp-toolkit.git", "giantswarm", "mcp-toolkit"},
		{"ssh without .git", "git@github.com:giantswarm/mcp-toolkit", "giantswarm", "mcp-toolkit"},
		{"https with .git", "https://github.com/giantswarm/mcp-toolkit.git", "giantswarm", "mcp-toolkit"},
		{"https without .git", "https://github.com/giantswarm/mcp-toolkit", "giantswarm", "mcp-toolkit"},
		{"https with token (align-files action shape)", "https://x-access-token:abc@github.com/giantswarm/mcp-toolkit.git", "giantswarm", "mcp-toolkit"},
		{"non-github remote", "git@gitlab.com:foo/bar.git", "", ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := initRepoWithRemote(t, tc.url)
			t.Chdir(dir)
			owner, repo := repoFromOriginRemote()
			if owner != tc.wantOwner || repo != tc.wantRepo {
				t.Fatalf("got (%q, %q), want (%q, %q)", owner, repo, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}

func Test_latestReleasedVersionFromAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/giantswarm/mcp-toolkit/tags" {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Unsorted, with non-semver and pre-release entries the picker must skip.
		_, _ = w.Write([]byte(`[
			{"name":"v0.1.0"},
			{"name":"v0.2.1"},
			{"name":"v0.2.0"},
			{"name":"v0.2.1-rc1"},
			{"name":"not-a-version"}
		]`))
	}))
	defer server.Close()

	t.Setenv("GS_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	t.Cleanup(func() { githubAPIBaseURL = origBaseURL })

	dir := initRepoWithRemote(t, "git@github.com:giantswarm/mcp-toolkit.git")
	t.Chdir(dir)

	got := latestReleasedVersionFromAPI()
	if got != "0.2.1" {
		t.Fatalf("got %q, want %q", got, "0.2.1")
	}
}

func Test_latestReleasedVersionFromAPI_noTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	t.Cleanup(func() { githubAPIBaseURL = origBaseURL })

	dir := initRepoWithRemote(t, "git@github.com:giantswarm/new-repo.git")
	t.Chdir(dir)

	if got := latestReleasedVersionFromAPI(); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func Test_latestReleasedVersionFromAPI_apiError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	t.Cleanup(func() { githubAPIBaseURL = origBaseURL })

	dir := initRepoWithRemote(t, "git@github.com:giantswarm/mcp-toolkit.git")
	t.Chdir(dir)

	if got := latestReleasedVersionFromAPI(); got != "" {
		t.Fatalf("got %q on API 500, want empty (callers treat empty as 'no baseline')", got)
	}
}

func initRepoWithRemote(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", url},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	return dir
}
