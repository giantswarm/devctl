package authstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

var testNow = time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

// fakeGitHub is GitHub's OAuth host and API in one server. The token
// endpoint answers its queued responses in order and repeats the last.
type fakeGitHub struct {
	t          *testing.T
	server     *httptest.Server
	tokenQueue []map[string]any
	tokenCalls atomic.Int32
	tokenForms []map[string]string
	user       string
}

func newFakeGitHub(t *testing.T, user string, tokenQueue ...map[string]any) *fakeGitHub {
	f := &fakeGitHub{t: t, tokenQueue: tokenQueue, user: user}
	mux := http.NewServeMux()
	mux.HandleFunc(githubDeviceCodePath, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != GitHubAppClientID {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"device_code": "dev-code", "user_code": "ABCD-1234",
			"verification_uri": f.server.URL + "/login/device", "expires_in": 900, "interval": 5,
		})
	})
	mux.HandleFunc(githubAccessPath, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		form := map[string]string{}
		for k := range r.Form {
			form[k] = r.Form.Get(k)
		}
		f.tokenForms = append(f.tokenForms, form)
		n := int(f.tokenCalls.Add(1)) - 1
		if n >= len(f.tokenQueue) {
			n = len(f.tokenQueue) - 1
		}
		writeJSON(w, http.StatusOK, f.tokenQueue[n])
	})
	mux.HandleFunc(githubUserPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.user+"-token" {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"login": f.user})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// newTestAuth is an Auth over a file store in a temp dir, a fixed clock at
// scale 0.001 and endpoints pointing at the fakes; the browser opener
// records the URL.
func newTestAuth(t *testing.T, github, circleci *httptest.Server, opener func(string) error) (*Auth, *FileStore, *bytes.Buffer) {
	store := &FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	endpoints := agentcli.DefaultEndpoints()
	if github != nil {
		endpoints.GitHubOAuthURL = github.URL
		endpoints.GitHubAPIURL = github.URL
	}
	if circleci != nil {
		endpoints.CircleCIOAuthURL = circleci.URL
		endpoints.CircleCIAPIURL = circleci.URL + "/api/v2"
	}
	var stderr bytes.Buffer
	if opener == nil {
		opener = func(string) error { return nil }
	}
	a, err := New(Config{
		Store:       store,
		Endpoints:   endpoints,
		Clock:       agentcli.NewClock(0.001, func() time.Time { return testNow }),
		OpenBrowser: opener,
		Stderr:      &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, store, &stderr
}

func TestLoginGitHubDeviceFlow(t *testing.T) {
	gh := newFakeGitHub(t, "octocat",
		map[string]any{"error": "authorization_pending"},
		map[string]any{"error": "slow_down"},
		map[string]any{"error": "authorization_pending"},
		map[string]any{"access_token": "octocat-token", "token_type": "bearer", "expires_in": 28800,
			"refresh_token": "ghr_refresh", "refresh_token_expires_in": 15897600, "scope": ""},
	)
	var opened string
	a, store, stderr := newTestAuth(t, gh.server, nil, func(u string) error { opened = u; return nil })

	id, err := a.LoginGitHub(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !id.Present || id.Login != "octocat" || id.Expired || !id.Refreshable {
		t.Fatalf("identity = %+v", id)
	}
	if gh.tokenCalls.Load() != 4 {
		t.Fatalf("token endpoint called %d times, want 4", gh.tokenCalls.Load())
	}
	if got := gh.tokenForms[0]; got["grant_type"] != grantTypeDeviceCode || got["device_code"] != "dev-code" || got["client_id"] != GitHubAppClientID {
		t.Fatalf("poll form = %v", got)
	}
	if opened != gh.server.URL+"/login/device" {
		t.Fatalf("browser opened %q", opened)
	}
	if out := stderr.String(); !strings.Contains(out, "ABCD-1234") || !strings.Contains(out, gh.server.URL+"/login/device") || strings.Contains(out, "octocat-token") {
		t.Fatalf("stderr = %q", out)
	}

	rec, err := store.Get(UserGitHub)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Token != "octocat-token" || rec.RefreshToken != "ghr_refresh" || rec.Login != "octocat" {
		t.Fatalf("record = %+v", rec)
	}
	if !rec.ExpiresAt.Equal(testNow.Add(8*time.Hour)) || !rec.RefreshExpiresAt.Equal(testNow.Add(15897600*time.Second)) {
		t.Fatalf("expiry = %s / %s", rec.ExpiresAt, rec.RefreshExpiresAt)
	}
}

func TestLoginGitHubDenied(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"access_denied", "denied"},
		{"expired_token", "expired"},
		{"incorrect_device_code", "incorrect_device_code"},
	} {
		gh := newFakeGitHub(t, "octocat", map[string]any{"error": tc.code})
		a, store, _ := newTestAuth(t, gh.server, nil, nil)
		if _, err := a.LoginGitHub(context.Background()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v", tc.code, err)
		}
		if _, err := store.Get(UserGitHub); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: a record was stored: %v", tc.code, err)
		}
	}
}

func TestRequireGitHub(t *testing.T) {
	valid := Record{Login: "octocat", Token: "octocat-token", ExpiresAt: testNow.Add(time.Hour), RefreshToken: "ghr_old", RefreshExpiresAt: testNow.Add(30 * 24 * time.Hour)}
	expiredRefreshable := valid
	expiredRefreshable.Token = "stale-token"
	expiredRefreshable.ExpiresAt = testNow.Add(-time.Minute)
	expiredAll := expiredRefreshable
	expiredAll.RefreshExpiresAt = testNow.Add(-time.Minute)
	noRefresh := Record{Login: "octocat", Token: "stale-token", ExpiresAt: testNow.Add(-time.Minute)}
	neverExpires := Record{Login: "octocat", Token: "classic-token"}

	cases := []struct {
		name         string
		record       *Record
		refresh      map[string]any
		wantToken    string
		wantAuth     bool
		wantRefreshN int32
	}{
		{name: "missing", wantAuth: true},
		{name: "valid", record: &valid, wantToken: "octocat-token"},
		{name: "never expires", record: &neverExpires, wantToken: "classic-token"},
		{name: "expired, refreshed", record: &expiredRefreshable,
			refresh:   map[string]any{"access_token": "fresh-token", "expires_in": 28800, "refresh_token": "ghr_new", "refresh_token_expires_in": 15897600},
			wantToken: "fresh-token", wantRefreshN: 1},
		{name: "expired, refresh refused", record: &expiredRefreshable,
			refresh: map[string]any{"error": "bad_refresh_token"}, wantAuth: true, wantRefreshN: 1},
		{name: "expired, refresh token expired", record: &expiredAll, wantAuth: true},
		{name: "expired, no refresh token", record: &noRefresh, wantAuth: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGitHub(t, "octocat", tc.refresh)
			a, store, _ := newTestAuth(t, gh.server, nil, nil)
			if tc.record != nil {
				if err := store.Set(UserGitHub, *tc.record); err != nil {
					t.Fatal(err)
				}
			}
			tok, err := a.RequireGitHub(context.Background())
			if tc.wantAuth {
				var authErr *AuthRequiredError
				if !errors.Is(err, ErrAuthRequired) || !errors.As(err, &authErr) || authErr.ExitCode() != agentcli.ExitAuthRequired {
					t.Fatalf("err = %v, want ErrAuthRequired", err)
				}
				if !strings.Contains(err.Error(), "devctl auth login") || strings.Contains(err.Error(), "token\"") || strings.Contains(err.Error(), "stale-token") {
					t.Fatalf("message = %q", err.Error())
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if tok.Value != tc.wantToken || tok.Login != "octocat" {
					t.Fatalf("token = %+v", tok)
				}
			}
			if gh.tokenCalls.Load() != tc.wantRefreshN {
				t.Fatalf("refresh called %d times, want %d", gh.tokenCalls.Load(), tc.wantRefreshN)
			}
			if tc.wantRefreshN > 0 {
				form := gh.tokenForms[0]
				if form["grant_type"] != grantTypeRefresh || form["refresh_token"] != "ghr_old" || form["client_id"] != GitHubAppClientID || form["client_secret"] != "" {
					t.Fatalf("refresh form = %v", form)
				}
			}
			if tc.wantToken == "fresh-token" {
				rec, err := store.Get(UserGitHub)
				if err != nil || rec.Token != "fresh-token" || rec.RefreshToken != "ghr_new" || rec.Login != "octocat" || !rec.ExpiresAt.Equal(testNow.Add(8*time.Hour)) {
					t.Fatalf("stored after refresh: %+v %v", rec, err)
				}
			}
		})
	}
}

func TestStatusAndErr(t *testing.T) {
	a, store, _ := newTestAuth(t, nil, nil, nil)

	status, err := a.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.GitHub.Present || status.CircleCI.Present || status.GitHub.Warnings == nil {
		t.Fatalf("empty status = %+v", status)
	}
	if err := status.Check(); !errors.Is(err, ErrAuthRequired) || !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("Check = %v", err)
	}

	if err := store.Set(UserGitHub, Record{Login: "octocat", Token: "t", ExpiresAt: testNow.Add(-time.Hour), RefreshToken: "r", RefreshExpiresAt: testNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(UserCircleCI, Record{Login: "octocat", Token: "c", ExpiresAt: testNow.Add(3 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	status, err = a.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.GitHub.Expired || !status.GitHub.Refreshable || !status.GitHub.Usable() {
		t.Fatalf("github = %+v", status.GitHub)
	}
	if len(status.CircleCI.Warnings) != 1 || !strings.Contains(status.CircleCI.Warnings[0], "3 day(s)") {
		t.Fatalf("circleci = %+v", status.CircleCI)
	}
	if err := status.Check(); err != nil {
		t.Fatalf("Check = %v", err)
	}

	if err := store.Set(UserCircleCI, Record{Login: "octocat", Token: "c", ExpiresAt: testNow.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	status, _ = a.Status()
	if err := status.Check(); err == nil || !strings.Contains(err.Error(), "CircleCI authentication required (the token expired): run `devctl auth login --circleci-only`.") {
		t.Fatalf("Check = %v", err)
	}

	b, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"token"`) || strings.Contains(string(b), `"refreshToken"`) {
		t.Fatalf("status carries token material: %s", b)
	}
}

// TestRenewGitHub: GitHub answered 401 to a token mid-run. The store is read
// again under the lock: a token another run renewed is used as it is, the
// refused one is refreshed, and without a usable refresh token the run needs
// a login.
func TestRenewGitHub(t *testing.T) {
	live := Record{Login: "octocat", Token: "refused-token", ExpiresAt: testNow.Add(time.Hour), RefreshToken: "ghr_old", RefreshExpiresAt: testNow.Add(30 * 24 * time.Hour)}
	renewedElsewhere := live
	renewedElsewhere.Token = "other-run-token"
	expiredRenewedElsewhere := renewedElsewhere
	expiredRenewedElsewhere.ExpiresAt = testNow.Add(-time.Minute)
	noRefresh := Record{Login: "octocat", Token: "refused-token", ExpiresAt: testNow.Add(time.Hour)}
	refreshExpired := live
	refreshExpired.RefreshExpiresAt = testNow.Add(-time.Minute)
	fresh := map[string]any{"access_token": "fresh-token", "expires_in": 28800, "refresh_token": "ghr_new", "refresh_token_expires_in": 15897600}

	cases := []struct {
		name         string
		record       *Record
		refresh      map[string]any
		wantToken    string
		wantCause    string
		wantRefreshN int32
	}{
		{name: "refused before its expiry, refreshed", record: &live, refresh: fresh, wantToken: "fresh-token", wantRefreshN: 1},
		{name: "renewed by another run", record: &renewedElsewhere, wantToken: "other-run-token"},
		{name: "renewed by another run, since expired", record: &expiredRenewedElsewhere, refresh: fresh, wantToken: "fresh-token", wantRefreshN: 1},
		{name: "refresh refused", record: &live, refresh: map[string]any{"error": "bad_refresh_token"}, wantCause: "GitHub refused the refresh", wantRefreshN: 1},
		{name: "no refresh token", record: &noRefresh, wantCause: "GitHub refused the token and it cannot be refreshed"},
		{name: "refresh token expired", record: &refreshExpired, wantCause: "GitHub refused the token and it cannot be refreshed"},
		{name: "logged out meanwhile", wantCause: causeNoToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGitHub(t, "octocat", tc.refresh)
			a, store, _ := newTestAuth(t, gh.server, nil, nil)
			if tc.record != nil {
				if err := store.Set(UserGitHub, *tc.record); err != nil {
					t.Fatal(err)
				}
			}
			tok, err := a.RenewGitHub(context.Background(), "refused-token")
			if tc.wantCause != "" {
				var authErr *AuthRequiredError
				if !errors.As(err, &authErr) || authErr.ExitCode() != agentcli.ExitAuthRequired || !strings.Contains(err.Error(), tc.wantCause) || !strings.Contains(err.Error(), "devctl auth login") {
					t.Fatalf("err = %v, want exit 8 naming %q", err, tc.wantCause)
				}
			} else if err != nil || tok.Value != tc.wantToken || tok.Login != "octocat" || tok.Source != SourceKeychain {
				t.Fatalf("token = %+v, err = %v, want %s", tok, err, tc.wantToken)
			}
			if gh.tokenCalls.Load() != tc.wantRefreshN {
				t.Fatalf("refresh called %d times, want %d", gh.tokenCalls.Load(), tc.wantRefreshN)
			}
			if tc.wantToken == "fresh-token" {
				if rec, err := store.Get(UserGitHub); err != nil || rec.Token != "fresh-token" || rec.RefreshToken != "ghr_new" {
					t.Fatalf("stored after the renewal: %+v %v", rec, err)
				}
			}
		})
	}
}

// Two runs refused for the same token at the same time spend the refresh
// token once: the second reads the first one's renewal under the lock. GitHub
// accepts a refresh token once, so a second refresh would fail.
func TestRenewGitHubConcurrentRunsRefreshOnce(t *testing.T) {
	gh := newFakeGitHub(t, "octocat",
		map[string]any{"access_token": "fresh-token", "expires_in": 28800, "refresh_token": "ghr_new", "refresh_token_expires_in": 15897600},
		map[string]any{"error": "bad_refresh_token"},
	)
	a, store, _ := newTestAuth(t, gh.server, nil, nil)
	if err := store.Set(UserGitHub, Record{Login: "octocat", Token: "refused-token", ExpiresAt: testNow.Add(-time.Second), RefreshToken: "ghr_old", RefreshExpiresAt: testNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// A second Auth over the same file is another devctl process.
	other, err := New(Config{Store: &FileStore{Path: store.Path}, Endpoints: a.endpoints, Clock: a.clock, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i, auth := range []*Auth{a, other} {
		wg.Go(func() {
			tok, err := auth.RenewGitHub(context.Background(), "refused-token")
			tokens[i], errs[i] = tok.Value, err
		})
	}
	wg.Wait()
	for i := range tokens {
		if errs[i] != nil || tokens[i] != "fresh-token" {
			t.Errorf("run %d: token %q, err %v", i, tokens[i], errs[i])
		}
	}
	if gh.tokenCalls.Load() != 1 {
		t.Fatalf("refresh called %d times, want 1", gh.tokenCalls.Load())
	}
}
