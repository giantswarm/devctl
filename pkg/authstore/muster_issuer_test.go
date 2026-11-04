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

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
)

func TestMusterRefreshKeepsOriginalIssuer(t *testing.T) {
	var exchanges atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, protectedResourcePath):
			writeJSON(w, http.StatusOK, protectedResource{Resource: server.URL + "/mcp", AuthorizationServers: []string{server.URL}})
		case r.URL.Path == authorizationServerPath:
			writeJSON(w, http.StatusOK, authorizationServer{Issuer: server.URL, AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token", CodeChallengeMethodsSupported: []string{"S256"}, ClientIDMetadataDocumentSupported: true})
		case r.URL.Path == "/token":
			exchanges.Add(1)
			writeJSON(w, http.StatusOK, oauthToken{AccessToken: "synthetic-new-access", RefreshToken: "synthetic-new-refresh", ExpiresIn: 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, store := newMusterAuth(t, func() time.Time { return testNow })
	record := Record{Token: "synthetic-old-access", RefreshToken: "synthetic-old-refresh", ExpiresAt: testNow.Add(-time.Hour), Endpoint: server.URL + "/mcp", Issuer: "https://original-issuer.example.invalid"}
	if err := store.Set(UserMuster, record); err != nil {
		t.Fatal(err)
	}
	_, err := a.RequireMuster(context.Background())
	if !errors.Is(err, ErrAuthRequired) {
		t.Errorf("changed issuer error = %v, want a new login", err)
	}
	if exchanges.Load() != 0 {
		t.Error("sent the original issuer's refresh token to the replacement server")
	}
	stored, err := store.Get(UserMuster)
	if err != nil || stored != record {
		t.Errorf("issuer refusal changed the stored record: %v", err)
	}
}

func TestMusterRefreshWithoutIssuerRequiresLogin(t *testing.T) {
	m := muster.Start(muster.Config{RefreshToken: "synthetic-refresh"})
	defer m.Close()
	a, store := newMusterAuth(t, func() time.Time { return testNow })
	record := Record{Token: "synthetic-access", RefreshToken: "synthetic-refresh", ExpiresAt: testNow.Add(-time.Hour), Endpoint: m.MCPURL()}
	if err := store.Set(UserMuster, record); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RequireMuster(context.Background()); !errors.Is(err, ErrAuthRequired) {
		t.Errorf("missing issuer error = %v, want a new login", err)
	}
	if len(m.Requests()) != 0 {
		t.Error("a token with no known issuer caused network access")
	}
}

func TestDiscoverMusterRejectsIssuerMismatch(t *testing.T) {
	for _, reported := range []string{"", "https://different.example.invalid"} {
		t.Run(reported, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, protectedResourcePath) {
					writeJSON(w, http.StatusOK, protectedResource{AuthorizationServers: []string{server.URL}})
					return
				}
				writeJSON(w, http.StatusOK, authorizationServer{Issuer: reported, AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token", CodeChallengeMethodsSupported: []string{"S256"}, ClientIDMetadataDocumentSupported: true})
			}))
			defer server.Close()
			a, _ := newMusterAuth(t, nil)
			if _, err := a.discoverMuster(context.Background(), server.URL+"/mcp"); err == nil {
				t.Error("accepted metadata that does not identify the requested issuer")
			}
		})
	}
}

func TestMusterRefreshSelectsStoredIssuer(t *testing.T) {
	var exchanges atomic.Int32
	var original *httptest.Server
	original = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == authorizationServerPath {
			writeJSON(w, http.StatusOK, authorizationServer{Issuer: original.URL, AuthorizationEndpoint: original.URL + "/authorize", TokenEndpoint: original.URL + "/token", CodeChallengeMethodsSupported: []string{"S256"}, ClientIDMetadataDocumentSupported: true})
			return
		}
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		exchanges.Add(1)
		if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "synthetic-original-refresh" {
			http.Error(w, "wrong refresh", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, oauthToken{AccessToken: "synthetic-original-access", RefreshToken: "synthetic-rotated-refresh", ExpiresIn: 3600})
	}))
	defer original.Close()
	var replacementCalls atomic.Int32
	var resource *httptest.Server
	resource = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, protectedResourcePath) {
			writeJSON(w, http.StatusOK, protectedResource{Resource: resource.URL + "/mcp", AuthorizationServers: []string{resource.URL, original.URL}})
			return
		}
		replacementCalls.Add(1)
		http.Error(w, "replacement issuer must not receive a refresh", http.StatusBadRequest)
	}))
	defer resource.Close()
	a, store := newMusterAuth(t, func() time.Time { return testNow })
	if err := store.Set(UserMuster, Record{Token: "stale", RefreshToken: "synthetic-original-refresh", ExpiresAt: testNow.Add(-time.Hour), Endpoint: resource.URL + "/mcp", Issuer: original.URL}); err != nil {
		t.Fatal(err)
	}
	token, err := a.RequireMuster(context.Background())
	if err != nil || token.Value != "synthetic-original-access" {
		t.Errorf("stored issuer refresh failed: %v", err)
	}
	if exchanges.Load() != 1 || replacementCalls.Load() != 0 {
		t.Errorf("original exchanges = %d, replacement requests = %d", exchanges.Load(), replacementCalls.Load())
	}
	stored, err := store.Get(UserMuster)
	if err != nil || stored.Issuer != original.URL || stored.RefreshToken != "synthetic-rotated-refresh" {
		t.Errorf("original issuer's renewed pair was not persisted: %v", err)
	}
}
