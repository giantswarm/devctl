package authstore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMusterUnrotatedRefreshKeepsExpiry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		refresh     string
		lifetime    int64
		wantRefresh string
		wantExpiry  time.Time
	}{
		{name: "retained token and deadline", wantRefresh: "synthetic-refresh", wantExpiry: testNow.Add(time.Hour)},
		{name: "retained token with updated lifetime", lifetime: 600, wantRefresh: "synthetic-refresh", wantExpiry: testNow.Add(10 * time.Minute)},
		{name: "rotated token with new deadline", refresh: "synthetic-new-refresh", lifetime: 7200, wantRefresh: "synthetic-new-refresh", wantExpiry: testNow.Add(2 * time.Hour)},
		{name: "rotated token without a known deadline", refresh: "synthetic-new-refresh", wantRefresh: "synthetic-new-refresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := testNow
			var exchanges atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasPrefix(r.URL.Path, protectedResourcePath):
					writeJSON(w, http.StatusOK, protectedResource{Resource: server.URL + "/mcp", AuthorizationServers: []string{server.URL}})
				case r.URL.Path == authorizationServerPath:
					writeJSON(w, http.StatusOK, authorizationServer{Issuer: server.URL, AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token", CodeChallengeMethodsSupported: []string{"S256"}, ClientIDMetadataDocumentSupported: true})
				case r.URL.Path == "/token":
					if exchanges.Add(1) > 1 {
						writeJSON(w, http.StatusBadRequest, oauthToken{Error: "invalid_grant"})
						return
					}
					writeJSON(w, http.StatusOK, oauthToken{AccessToken: "synthetic-renewed-access", ExpiresIn: 60, RefreshToken: tc.refresh, RefreshTokenExpiresIn: tc.lifetime})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, store := newMusterAuth(t, func() time.Time { return now })
			if err := store.Set(UserMuster, Record{Token: "stale", RefreshToken: "synthetic-refresh", ExpiresAt: now.Add(-time.Hour), RefreshExpiresAt: now.Add(time.Hour), Endpoint: server.URL + "/mcp", Issuer: server.URL}); err != nil {
				t.Fatal(err)
			}
			if _, err := a.RequireMuster(context.Background()); err != nil {
				t.Fatal(err)
			}
			stored, err := store.Get(UserMuster)
			if err != nil || stored.RefreshToken != tc.wantRefresh || !stored.RefreshExpiresAt.Equal(tc.wantExpiry) {
				t.Errorf("refresh token expiry contract was not persisted: %v", err)
			}
			if tc.wantExpiry.IsZero() {
				return
			}
			now = tc.wantExpiry.Add(time.Minute)
			status, err := a.Status()
			if err != nil || status.Muster.Refreshable || status.Muster.Usable() {
				t.Errorf("an expired access/refresh pair was reported usable: %v", err)
			}
			if _, err := a.RequireMuster(context.Background()); !errors.Is(err, ErrAuthRequired) {
				t.Errorf("expired refresh error = %v, want a new login", err)
			}
			if exchanges.Load() != 1 {
				t.Errorf("token exchanges = %d, want no exchange after known refresh expiry", exchanges.Load())
			}
		})
	}
}
