package authstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// fakeCircleCI is the OAuth issuer and API v2 in one server. It checks the
// registration, remembers the challenge of the authorization request the
// browser (the test's opener) brings, and verifies the PKCE exchange.
type fakeCircleCI struct {
	t             *testing.T
	server        *httptest.Server
	registrations atomic.Int32
	redirectURIs  []string
	challenge     string
	code          string
	tokenForm     map[string]string
}

func newFakeCircleCI(t *testing.T) *fakeCircleCI {
	f := &fakeCircleCI{t: t, code: "auth-code-1"}
	mux := http.NewServeMux()
	mux.HandleFunc(circleCIRegisterPath, func(w http.ResponseWriter, r *http.Request) {
		var reg clientRegistration
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if reg.ClientName != "devctl" || len(reg.RedirectURIs) != 1 || reg.GrantTypes[0] != "authorization_code" || reg.ResponseTypes[0] != "code" || reg.TokenEndpointAuthMethod != "none" {
			http.Error(w, "bad registration", http.StatusBadRequest)
			return
		}
		f.redirectURIs = append(f.redirectURIs, reg.RedirectURIs[0])
		n := f.registrations.Add(1)
		writeJSON(w, http.StatusCreated, map[string]any{
			"client_id": "client-" + string(rune('0'+n)), "client_id_issued_at": testNow.Unix(),
			"client_name": reg.ClientName, "redirect_uris": reg.RedirectURIs,
			"grant_types": reg.GrantTypes, "response_types": reg.ResponseTypes, "token_endpoint_auth_method": "none",
		})
	})
	mux.HandleFunc(circleCIExchangePath, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.tokenForm = map[string]string{}
		for k := range r.Form {
			f.tokenForm[k] = r.Form.Get(k)
		}
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge || r.Form.Get("code") != f.code {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "ccipat_secret", "token_type": "Bearer", "expires_in": 7776000})
	})
	mux.HandleFunc("/api/v2"+circleCIMePath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Circle-Token") != "ccipat_secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"login": "octocat", "id": "u-1", "name": "Octo Cat"})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// browser is the opener that plays the human: it reads the authorization
// URL, keeps the challenge for the token endpoint and follows the redirect
// with the code.
func (f *fakeCircleCI) browser(mutate func(q url.Values)) func(string) error {
	return func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		q := u.Query()
		if !strings.HasPrefix(raw, f.server.URL+circleCIAuthorizePath) || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" {
			f.t.Errorf("authorization URL = %s", raw)
		}
		f.challenge = q.Get("code_challenge")
		redirect, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			return err
		}
		back := url.Values{"code": {f.code}, "state": {q.Get("state")}}
		if mutate != nil {
			mutate(back)
		}
		redirect.RawQuery = back.Encode()
		go func() {
			resp, err := http.Get(redirect.String())
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginCircleCIRegistersOncePerDevice(t *testing.T) {
	cci := newFakeCircleCI(t)
	a, store, stderr := newTestAuth(t, nil, cci.server, nil)
	a.openBrowser = cci.browser(nil)

	id, err := a.LoginCircleCI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !id.Present || id.Login != "octocat" || id.Expired || id.Refreshable || id.ExpiresAt == nil || !id.ExpiresAt.Equal(testNow.Add(90*24*time.Hour)) {
		t.Fatalf("identity = %+v", id)
	}
	if len(id.Warnings) != 0 {
		t.Fatalf("a fresh token warns: %v", id.Warnings)
	}
	if out := stderr.String(); !strings.Contains(out, cci.server.URL+circleCIAuthorizePath) || strings.Contains(out, "ccipat_secret") {
		t.Fatalf("stderr = %q", out)
	}
	if cci.tokenForm["grant_type"] != "authorization_code" || cci.tokenForm["client_id"] != "client-1" || cci.tokenForm["redirect_uri"] != cci.redirectURIs[0] {
		t.Fatalf("token form = %v", cci.tokenForm)
	}
	if !strings.HasPrefix(cci.redirectURIs[0], "http://127.0.0.1:") || !strings.HasSuffix(cci.redirectURIs[0], loopbackCallbackPath) {
		t.Fatalf("redirect URI = %q", cci.redirectURIs[0])
	}

	first, err := store.Get(UserCircleCI)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token != "ccipat_secret" || first.ClientID != "client-1" || first.RedirectURI != cci.redirectURIs[0] || first.Login != "octocat" {
		t.Fatalf("record = %+v", first)
	}

	// The second login reuses the device's client and its loopback address.
	cci.code = "auth-code-2"
	if _, err := a.LoginCircleCI(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cci.registrations.Load() != 1 {
		t.Fatalf("registered %d times, want 1", cci.registrations.Load())
	}
	second, _ := store.Get(UserCircleCI)
	if second.ClientID != "client-1" || second.RedirectURI != first.RedirectURI {
		t.Fatalf("second record = %+v", second)
	}
	if cci.tokenForm["code"] != "auth-code-2" {
		t.Fatalf("second exchange used code %q", cci.tokenForm["code"])
	}
}

func TestLoginCircleCIRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(q url.Values)
		want   string
	}{
		{name: "state mismatch", mutate: func(q url.Values) { q.Set("state", "forged") }, want: "state"},
		{name: "refused in the browser", mutate: func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }, want: "access_denied"},
		{name: "wrong code", mutate: func(q url.Values) { q.Set("code", "wrong") }, want: "invalid_grant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cci := newFakeCircleCI(t)
			a, store, _ := newTestAuth(t, nil, cci.server, nil)
			a.openBrowser = cci.browser(tc.mutate)
			_, err := a.LoginCircleCI(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if _, err := store.Get(UserCircleCI); !errors.Is(err, ErrNotFound) {
				t.Fatalf("a record was stored: %v", err)
			}
		})
	}
}

func TestLoginCircleCITimesOut(t *testing.T) {
	cci := newFakeCircleCI(t)
	a, _, _ := newTestAuth(t, nil, cci.server, nil)
	// The human never comes back; at scale 0.001 the ten minutes are 600 ms.
	_, err := a.LoginCircleCI(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not completed in time") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequireCircleCI(t *testing.T) {
	cases := []struct {
		name        string
		record      *Record
		wantAuth    bool
		wantWarning string
	}{
		{name: "missing", wantAuth: true},
		{name: "expired", record: &Record{Token: "t", ExpiresAt: testNow.Add(-time.Second)}, wantAuth: true},
		{name: "within the skew", record: &Record{Token: "t", ExpiresAt: testNow.Add(10 * time.Second)}, wantAuth: true},
		{name: "valid, far away", record: &Record{Token: "t", ExpiresAt: testNow.Add(60 * 24 * time.Hour)}},
		{name: "valid, expires in 7 days", record: &Record{Token: "t", ExpiresAt: testNow.Add(7 * 24 * time.Hour)}, wantWarning: "7 day(s)"},
		{name: "valid, expires tomorrow", record: &Record{Token: "t", ExpiresAt: testNow.Add(20 * time.Hour)}, wantWarning: "1 day(s)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, store, _ := newTestAuth(t, nil, nil, nil)
			if tc.record != nil {
				tc.record.Login = "octocat"
				if err := store.Set(UserCircleCI, *tc.record); err != nil {
					t.Fatal(err)
				}
			}
			tok, err := a.RequireCircleCI(context.Background())
			if tc.wantAuth {
				var authErr *AuthRequiredError
				if !errors.As(err, &authErr) || authErr.ExitVerdict() != agentcli.VerdictAuthRequired || !strings.Contains(err.Error(), "devctl auth login") {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tok.Value != "t" || tok.Login != "octocat" {
				t.Fatalf("token = %+v", tok)
			}
			if tc.wantWarning == "" && tok.Warning != "" {
				t.Fatalf("unexpected warning %q", tok.Warning)
			}
			if tc.wantWarning != "" && (!strings.Contains(tok.Warning, tc.wantWarning) || !strings.Contains(tok.Warning, "devctl auth login --circleci-only")) {
				t.Fatalf("warning = %q", tok.Warning)
			}
		})
	}
}
