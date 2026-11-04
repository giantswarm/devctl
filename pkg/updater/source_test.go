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

	"github.com/giantswarm/go-selfupdate"
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

// releaseJSON is a release of giantswarm/devctl carrying this platform's
// binary, or none when complete is false.
func releaseJSON(id int64, tag string, complete bool) map[string]any {
	rel := map[string]any{"id": id, "tag_name": tag, "html_url": repositoryURL + "/releases/tag/" + tag, "assets": []map[string]any{}}
	if complete {
		rel["assets"] = []map[string]any{{"id": id * 10, "name": binaryAsset(), "browser_download_url": "https://example.test/" + binaryAsset()}}
	}
	return rel
}

// flakyGitHub serves giantswarm/devctl's release lists, one per request and
// the last one from then on, and its latest release with latestStatus.
// listed counts the list requests.
type flakyGitHub struct {
	lists        [][]map[string]any
	latestStatus int
	latest       map[string]any
	listed       int
}

func (g *flakyGitHub) source(t *testing.T) *githubSource {
	t.Helper()
	prev := listRetryDelay
	listRetryDelay = 0
	t.Cleanup(func() { listRetryDelay = prev })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases", func(w http.ResponseWriter, _ *http.Request) {
		g.listed++
		_ = json.NewEncoder(w).Encode(g.lists[min(g.listed, len(g.lists))-1])
	})
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(g.latestStatus)
		_ = json.NewEncoder(w).Encode(g.latest)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, err := newGitHubSource(selfupdate.ParseSlug("giantswarm/devctl"), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (g *flakyGitHub) detectLatest(t *testing.T) (*selfupdate.Release, bool) {
	t.Helper()
	up, err := selfupdate.NewUpdater(selfupdate.Config{Source: g.source(t)})
	if err != nil {
		t.Fatal(err)
	}
	rel, found, err := up.DetectLatest(context.Background(), selfupdate.ParseSlug("giantswarm/devctl"))
	if err != nil {
		t.Fatalf("DetectLatest: %v", err)
	}
	return rel, found
}

// GitHub intermittently lists no release; the latest release is the most
// recently published one, here a fix of an older line.
func TestAnEmptyListIsAskedForAgain(t *testing.T) {
	g := &flakyGitHub{
		lists:        [][]map[string]any{{}, {releaseJSON(3, "v1.0.1", true), releaseJSON(2, "v2.0.0", true)}},
		latestStatus: http.StatusOK,
		latest:       releaseJSON(3, "v1.0.1", true),
	}
	rel, found := g.detectLatest(t)
	if !found || rel.Version() != newerVersion {
		t.Fatalf("DetectLatest = %v, %v; want %s", rel, found, newerVersion)
	}
	if g.listed != 2 {
		t.Errorf("the list was asked for %d times, want 2", g.listed)
	}
}

func TestTheLatestReleaseIsFoundWhenTheListStaysEmpty(t *testing.T) {
	g := &flakyGitHub{lists: [][]map[string]any{{}}, latestStatus: http.StatusOK, latest: releaseJSON(2, "v2.0.0", true)}
	rel, found := g.detectLatest(t)
	if !found || rel.Version() != newerVersion {
		t.Fatalf("DetectLatest = %v, %v; want %s", rel, found, newerVersion)
	}
	if g.listed != 1+listRetries {
		t.Errorf("the list was asked for %d times, want %d", g.listed, 1+listRetries)
	}
}

// A release's assets upload after it is published.
func TestALatestReleaseWithoutAssetsFallsBackToTheListed(t *testing.T) {
	g := &flakyGitHub{
		lists:        [][]map[string]any{{releaseJSON(1, "v1.0.0", true)}},
		latestStatus: http.StatusOK,
		latest:       releaseJSON(2, "v2.0.0", false),
	}
	rel, found := g.detectLatest(t)
	if !found || rel.Version() != currentVersion {
		t.Fatalf("DetectLatest = %v, %v; want %s", rel, found, currentVersion)
	}
}

func TestListReleasesWithTheLatestRelease(t *testing.T) {
	listed := []map[string]any{releaseJSON(1, "v1.0.0", true)}
	for _, tc := range []struct {
		name         string
		list         []map[string]any
		latestStatus int
		want         []string
		wantErr      bool
	}{
		{"latest added", listed, http.StatusOK, []string{"v1.0.0", "v2.0.0"}, false},
		{"latest listed once", []map[string]any{releaseJSON(2, "v2.0.0", true)}, http.StatusOK, []string{"v2.0.0"}, false},
		{"no release yet", []map[string]any{}, http.StatusNotFound, []string{}, false},
		{"latest fails, the list stands", listed, http.StatusBadGateway, []string{"v1.0.0"}, false},
		{"latest fails, nothing listed", []map[string]any{}, http.StatusBadGateway, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &flakyGitHub{lists: [][]map[string]any{tc.list}, latestStatus: tc.latestStatus, latest: releaseJSON(2, "v2.0.0", true)}
			rels, err := g.source(t).ListReleases(context.Background(), selfupdate.ParseSlug("giantswarm/devctl"))
			if tc.wantErr {
				if err == nil {
					t.Fatal("ListReleases answered, want an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tags := []string{}
			for _, rel := range rels {
				tags = append(tags, rel.GetTagName())
			}
			if !slices.Equal(tags, tc.want) {
				t.Errorf("listed %v, want %v", tags, tc.want)
			}
		})
	}
}

// A newer release is still uploading, while the highest version with a
// platform binary is on a later list page, behind a maintenance release.
func TestLatestVersionIncludesLaterReleasePages(t *testing.T) {
	mux := http.NewServeMux()
	var pages []string
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases", func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %q, want 100", got)
		}
		if r.URL.Query().Get("page") == "2" {
			_ = json.NewEncoder(w).Encode([]map[string]any{releaseJSON(2, "v2.0.0", true)})
			return
		}
		w.Header().Set("Link", "<http://"+r.Host+"/repos/giantswarm/devctl/releases?page=2>; rel=\"next\"")
		_ = json.NewEncoder(w).Encode([]map[string]any{releaseJSON(3, "v3.0.0", false), releaseJSON(1, "v1.0.1", true)})
	})
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseJSON(3, "v3.0.0", false))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := newGitHubUpdater(t, &githubAPI{url: srv.URL}, "", nil)

	version, err := u.GetLatestFromSource()
	if !IsHasNewVersion(err) || version != newerVersion {
		t.Fatalf("GetLatestFromSource = %q, %v; want %s from the later page", version, err, newerVersion)
	}
	if !slices.Equal(pages, []string{"", "2"}) {
		t.Errorf("release pages = %v, want the first and second page once each", pages)
	}
}

func TestLatestVersionReturnsALaterPageError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Link", "<http://"+r.Host+"/repos/giantswarm/devctl/releases?page=2>; rel=\"next\"")
		_ = json.NewEncoder(w).Encode([]map[string]any{releaseJSON(1, "v1.0.1", true)})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := newGitHubUpdater(t, &githubAPI{url: srv.URL}, "", nil)

	version, err := u.GetLatestFromSource()
	if err == nil || !strings.Contains(err.Error(), "502") || version != "" {
		t.Fatalf("GetLatestFromSource = %q, %v; want the later page's error, not a partial version", version, err)
	}
}

func TestReleasePaginationHasALookupDeadline(t *testing.T) {
	previous := releaseLookupTimeout
	releaseLookupTimeout = 20 * time.Millisecond
	t.Cleanup(func() { releaseLookupTimeout = previous })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/giantswarm/devctl/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Link", "<http://"+r.Host+"/repos/giantswarm/devctl/releases?page=2>; rel=\"next\"")
		_ = json.NewEncoder(w).Encode([]map[string]any{releaseJSON(1, "v1.0.1", true)})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := newGitHubUpdater(t, &githubAPI{url: srv.URL}, "", nil)

	version, err := u.GetLatestFromSource()
	if !errors.Is(err, context.DeadlineExceeded) || version != "" {
		t.Fatalf("GetLatestFromSource = %q, %v; want the lookup deadline, not a partial version", version, err)
	}
}

func TestTheRetryEndsWithTheContext(t *testing.T) {
	g := &flakyGitHub{lists: [][]map[string]any{{}}, latestStatus: http.StatusOK, latest: releaseJSON(2, "v2.0.0", true)}
	s := g.source(t)
	listRetryDelay = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.ListReleases(ctx, selfupdate.ParseSlug("giantswarm/devctl")); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("ListReleases: %v, want the context's deadline", err)
	}
}
