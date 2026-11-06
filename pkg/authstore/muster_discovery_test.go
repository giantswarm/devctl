package authstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverMusterIssuerWithPath(t *testing.T) {
	for _, tc := range []struct {
		name, issuerPath string
		openid           bool
	}{
		{name: "root"},
		{name: "tenant", issuerPath: "/tenant"},
		{name: "nested tenant", issuerPath: "/tenant/nested"},
		{name: "escaped tenant", issuerPath: "/tenant%2Fsegment"},
		{name: "legacy OpenID", issuerPath: "/tenant", openid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				metadataPath := authorizationServerPath + tc.issuerPath
				if tc.openid {
					metadataPath = tc.issuerPath + openIDConfigurationPath
				}
				switch {
				case strings.HasPrefix(r.URL.Path, protectedResourcePath):
					writeJSON(w, http.StatusOK, protectedResource{Resource: server.URL + "/mcp", AuthorizationServers: []string{server.URL + tc.issuerPath}})
				case r.URL.EscapedPath() == metadataPath:
					writeJSON(w, http.StatusOK, authorizationServer{Issuer: server.URL + tc.issuerPath, AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token", CodeChallengeMethodsSupported: []string{"S256"}, ClientIDMetadataDocumentSupported: true})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, _ := newMusterAuth(t, nil)
			discovered, err := a.discoverMuster(context.Background(), server.URL+"/mcp")
			if err != nil {
				t.Fatalf("issuer could not be discovered: %v", err)
			}
			if discovered.Server.Issuer != server.URL+tc.issuerPath {
				t.Fatal("discovered the wrong issuer")
			}
		})
	}
}
