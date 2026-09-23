package updater

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

// githubAPI is a GitHub API double serving one release of giantswarm/devctl
// carrying this platform's binary, and recording the Authorization header of
// every request.
type githubAPI struct {
	url   string
	auths []string
}

func newGitHubAPI(t *testing.T, tag string) *githubAPI {
	t.Helper()
	api := &githubAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases", func(w http.ResponseWriter, r *http.Request) {
		api.auths = append(api.auths, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id": 1, "tag_name": tag, "name": tag, "html_url": repositoryURL + "/releases/tag/" + tag,
			"assets": []map[string]any{{"id": binaryAssetID, "name": binaryAsset(), "size": 3, "browser_download_url": "https://example.test/" + binaryAsset()}},
		}})
	})
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		api.auths = append(api.auths, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "bin")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	api.url = srv.URL
	return api
}

// newGitHubUpdater is an Updater for currentVersion reading api with token.
func newGitHubUpdater(t *testing.T, api *githubAPI, cacheDir string, token func(context.Context) (string, error)) *Updater {
	t.Helper()
	u, err := New(Config{
		GithubToken:    token,
		GitHubAPIURL:   api.url,
		CurrentVersion: currentVersion,
		RepositoryURL:  repositoryURL,
		CacheDir:       cacheDir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return u
}

func TestGitHubIsReadWithTheTokenAskedForOnce(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_environment")
	api := newGitHubAPI(t, "v"+newerVersion)
	asked := 0
	u := newGitHubUpdater(t, api, "", func(context.Context) (string, error) {
		asked++
		return "ghu_app", nil
	})

	for range 2 {
		version, err := u.GetLatestFromSource()
		if !IsHasNewVersion(err) || version != newerVersion {
			t.Fatalf("GetLatestFromSource = %q, %v; want %s reported as new", version, err, newerVersion)
		}
	}
	if asked != 1 {
		t.Errorf("the token was asked for %d times, want once", asked)
	}
	if want := []string{"Bearer ghu_app", "Bearer ghu_app"}; !slices.Equal(api.auths, want) {
		t.Errorf("Authorization headers %q, want %q", api.auths, want)
	}
}

func TestGitHubWithoutATokenIsReadAnonymously(t *testing.T) {
	// The library's own GitHub source would pick this up; the empty token
	// the caller returns must win.
	t.Setenv("GITHUB_TOKEN", "ghp_environment")

	for name, token := range map[string]func(context.Context) (string, error){
		"no token func": nil,
		"empty token":   func(context.Context) (string, error) { return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			api := newGitHubAPI(t, "v"+newerVersion)
			u := newGitHubUpdater(t, api, "", token)

			if _, err := u.GetLatestFromSource(); !IsHasNewVersion(err) {
				t.Fatalf("GetLatestFromSource: %v", err)
			}
			if want := []string{""}; !slices.Equal(api.auths, want) {
				t.Errorf("Authorization headers %q, want none", api.auths)
			}
		})
	}
}

func TestGitHubTokenErrorIsReturnedWithoutARequest(t *testing.T) {
	api := newGitHubAPI(t, "v"+newerVersion)
	u := newGitHubUpdater(t, api, "", func(context.Context) (string, error) {
		return "", errors.New("the keychain is locked")
	})

	_, err := u.GetLatestFromSource()
	if err == nil || !strings.Contains(err.Error(), "the keychain is locked") {
		t.Fatalf("GetLatestFromSource: %v, want the token's error", err)
	}
	if len(api.auths) != 0 {
		t.Errorf("%d request(s) went to GitHub without the token", len(api.auths))
	}
}

func TestGetLatestWithAFreshCacheDoesNotAskForTheToken(t *testing.T) {
	dir := t.TempDir()
	writeCache(t, dir, cache{LastUpdate: time.Now().UTC(), LatestVersion: cachedVersion})
	api := newGitHubAPI(t, "v"+newerVersion)
	u := newGitHubUpdater(t, api, dir, func(context.Context) (string, error) {
		t.Error("the token was asked for although the cache is fresh")
		return "", nil
	})

	version, err := u.GetLatest()
	if !IsHasNewVersion(err) || version != cachedVersion {
		t.Fatalf("GetLatest = %q, %v; want the cached %s", version, err, cachedVersion)
	}
	if len(api.auths) != 0 {
		t.Errorf("%d request(s) went to GitHub although the cache is fresh", len(api.auths))
	}
}

func TestGitHubSourceDownloadsWithTheSameToken(t *testing.T) {
	api := newGitHubAPI(t, "v"+newerVersion)
	asked := 0
	s, err := newGitHubSource(selfupdate.ParseSlug("giantswarm/devctl"), api.url, func(context.Context) (string, error) {
		asked++
		return "ghu_app", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := s.ListReleases(ctx, selfupdate.ParseSlug("giantswarm/devctl")); err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	rc, err := s.DownloadReleaseAsset(ctx, nil, binaryAssetID)
	if err != nil {
		t.Fatalf("DownloadReleaseAsset: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if got, _ := io.ReadAll(rc); string(got) != "bin" {
		t.Errorf("downloaded %q, want the asset", got)
	}
	if asked != 1 {
		t.Errorf("the token was asked for %d times, want once", asked)
	}
	if want := []string{"Bearer ghu_app", "Bearer ghu_app"}; !slices.Equal(api.auths, want) {
		t.Errorf("Authorization headers %q, want %q", api.auths, want)
	}
}

func TestNewRejectsAWrongGitHubAPIURL(t *testing.T) {
	_, err := New(Config{CurrentVersion: currentVersion, RepositoryURL: repositoryURL, GitHubAPIURL: "://no-scheme"})
	if !IsInvalidConfig(err) {
		t.Errorf("New: %v, want an invalid config", err)
	}
}
