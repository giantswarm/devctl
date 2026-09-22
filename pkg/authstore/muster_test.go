package authstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// browser fetches the authorization URL the way a signed-in person's browser
// would: the mock redirects straight back to devctl's loopback callback.
func browser(url string) error {
	go func() {
		resp, err := http.Get(url) //nolint:gosec // the test's own mock URL
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	return nil
}

func newMusterAuth(t *testing.T, now func() time.Time) (*Auth, *FileStore) {
	store := &FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	a, err := New(Config{
		Store:       store,
		Clock:       agentcli.NewClock(0.001, now),
		OpenBrowser: browser,
		Stderr:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, store
}

// The login discovers the authorization server from the endpoint, registers
// devctl once per device, binds the token to the endpoint and stores the
// record with the endpoint and the issuer; the second login reuses the
// client. RequireMuster returns the token while it is valid, refreshes it
// with the refresh token when it expired, and is exit 8 without a record.
func TestLoginMusterAndRequire(t *testing.T) {
	m := muster.Start(muster.Config{Login: "octocat@example.com"})
	defer m.Close()
	clock := testNow
	a, store := newMusterAuth(t, func() time.Time { return clock })
	ctx := context.Background()

	if _, err := a.RequireMuster(ctx); !errors.Is(err, ErrAuthRequired) || !strings.Contains(err.Error(), hintLoginMuster) {
		t.Fatalf("RequireMuster without a record = %v", err)
	}

	id, err := a.LoginMuster(ctx, m.MCPURL())
	if err != nil {
		t.Fatal(err)
	}
	if !id.Present || id.Login != "octocat@example.com" || !id.Refreshable || id.Expired || id.Endpoint != m.MCPURL() {
		t.Fatalf("identity = %+v", id)
	}
	record, err := store.Get(UserMuster)
	if err != nil {
		t.Fatal(err)
	}
	if record.Token != "muster-access-2" || record.RefreshToken != "muster-refresh-2" || record.ClientID != muster.ClientID || record.Issuer != m.URL || record.Endpoint != m.MCPURL() {
		t.Fatalf("record = %+v", record)
	}
	if !strings.HasPrefix(record.RedirectURI, "http://127.0.0.1:") || !strings.HasSuffix(record.RedirectURI, loopbackCallbackPath) {
		t.Fatalf("redirect URI = %q", record.RedirectURI)
	}
	if got := record.ExpiresAt; !got.Equal(testNow.Add(time.Hour)) {
		t.Fatalf("expiry = %s", got)
	}

	// The second login reuses the registered client.
	if _, err := a.LoginMuster(ctx, m.MCPURL()); err != nil {
		t.Fatal(err)
	}
	registrations := 0
	for _, r := range m.Requests() {
		if r.Path == "/oauth/register" {
			registrations++
		}
	}
	if registrations != 1 {
		t.Fatalf("registered %d times, want once", registrations)
	}

	token, err := a.RequireMuster(ctx)
	if err != nil || token.Value == "" || token.Endpoint != m.MCPURL() || token.Login != "octocat@example.com" {
		t.Fatalf("RequireMuster = %+v, %v", token, err)
	}

	// Past the expiry the refresh token buys a new pair without a human.
	clock = testNow.Add(2 * time.Hour)
	refreshed, err := a.RequireMuster(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Value == token.Value || refreshed.Endpoint != m.MCPURL() {
		t.Fatalf("refreshed = %+v, before = %+v", refreshed, token)
	}
	after, _ := store.Get(UserMuster)
	if after.RefreshToken == record.RefreshToken || after.Issuer != m.URL || after.ClientID != muster.ClientID {
		t.Fatalf("record after the refresh = %+v", after)
	}

	// Without a refresh token an expired record is exit 8.
	after.RefreshToken = ""
	if err := store.Set(UserMuster, after); err != nil {
		t.Fatal(err)
	}
	clock = testNow.Add(4 * time.Hour)
	if _, err := a.RequireMuster(ctx); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("RequireMuster with an expired, unrefreshable record = %v", err)
	}
}

// An authorization server without S256 PKCE is refused before any browser
// opens, and an endpoint without protected-resource metadata is an error
// naming the discovery.
func TestDiscoverMusterRefusals(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"resource": srv.URL + "/mcp", "authorization_servers": []string{srv.URL}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer": srv.URL, "authorization_endpoint": srv.URL + "/a", "token_endpoint": srv.URL + "/t",
			"code_challenge_methods_supported": []string{"plain"},
		})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	a, _ := newMusterAuth(t, nil)

	_, err := a.LoginMuster(context.Background(), srv.URL+"/mcp")
	if err == nil || !strings.Contains(err.Error(), "S256") {
		t.Fatalf("err = %v, want the S256 refusal", err)
	}

	bare := httptest.NewServer(http.NotFoundHandler())
	defer bare.Close()
	_, err = a.LoginMuster(context.Background(), bare.URL+"/mcp")
	if err == nil || !strings.Contains(err.Error(), "discovering the authorization server") {
		t.Fatalf("err = %v, want a discovery error", err)
	}

	if _, err := a.LoginMuster(context.Background(), ""); err == nil {
		t.Fatal("an empty endpoint was accepted")
	}
}
