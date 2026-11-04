package rerun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

// Exercise the rerun client's actual keychain-renewal wiring with a valid,
// unexpired token issued before a permission grant. All records and HTTP
// endpoints are fixtures; the personal identity must never try that refresh.
func TestRerunPermissionGrantRefresh(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owner  string
		source string
	}{
		{name: "case 0: App login refreshes", owner: "giantswarm", source: authstore.SourceKeychain},
		{name: "case 1: personal login stays refused", owner: "other", source: authexec.SourceGHLogin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts, probes, refreshes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch req.Method + " " + req.URL.Path {
				case "GET /apps/giantswarm-devctl":
					probes.Add(1)
					if req.Header.Get("Authorization") != "" {
						t.Error("public lookup carried a credential")
					}
					_, _ = io.WriteString(w, `{"permissions":{"repository_hooks":"write"}}`)
				case "POST /login/oauth/access_token":
					refreshes.Add(1)
					if err := req.ParseForm(); err != nil {
						t.Error(err)
					}
					if req.Form.Get("grant_type") != "refresh_token" || req.Form.Get("refresh_token") != "ghr_fixture" {
						t.Error("unexpected OAuth grant")
					}
					_, _ = io.WriteString(w, `{"access_token":"ghu_fresh","refresh_token":"ghr_next","expires_in":28800,"refresh_token_expires_in":15897600}`)
				case "GET /repos/" + tc.owner + "/r/hooks/1/deliveries":
					attempts.Add(1)
					if req.Header.Get("Authorization") != "Bearer ghu_fresh" {
						w.Header().Set("X-Accepted-GitHub-Permissions", "repository_hooks=read")
						w.WriteHeader(http.StatusNotFound)
						_, _ = io.WriteString(w, `{"message":"Not Found"}`)
						return
					}
					_, _ = io.WriteString(w, `[{"id":42}]`)
				default:
					t.Error("unexpected fixture request")
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			t.Cleanup(server.Close)
			t.Setenv(agentcli.EnvKeyringFile, filepath.Join(t.TempDir(), "keyring.json"))
			t.Setenv(agentcli.EnvGitHubOAuthURL, server.URL)
			t.Setenv(agentcli.EnvGitHubAPIURL, server.URL)
			t.Setenv("DEVCTL_TIME_SCALE", "1")
			endpoints := agentcli.EndpointsFromEnv()
			store := authstore.OpenStore(endpoints)
			record := authstore.Record{Login: "fixture", Token: "ghu_stale", RefreshToken: "ghr_fixture", ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour)}
			if err := store.Set(authstore.UserGitHub, record); err != nil {
				t.Fatal(err)
			}
			identity := func(context.Context) (authstore.Token, error) {
				return authstore.Token{Value: record.Token, Source: tc.source}, nil
			}
			client, token, err := NewGitHub(context.Background(), tc.owner, identity, identity, endpoints, server.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			if token.Source != tc.source {
				t.Fatal("client changed the chosen identity")
			}
			deliveries, err := client.ListHookDeliveries(context.Background(), tc.owner, "r", 1)
			if tc.source == authexec.SourceGHLogin {
				var refusal *github.ErrorResponse
				if !errors.As(err, &refusal) || refusal.Response.StatusCode != http.StatusNotFound || attempts.Load() != 1 || probes.Load() != 0 || refreshes.Load() != 0 {
					t.Fatalf("personal refusal: error = %v, attempts = %d, probes = %d, refreshes = %d", err, attempts.Load(), probes.Load(), refreshes.Load())
				}
			} else {
				if err != nil || len(deliveries) != 1 || deliveries[0].GetID() != 42 || attempts.Load() != 2 || probes.Load() != 1 || refreshes.Load() != 1 {
					t.Fatalf("App refresh: error = %v, attempts = %d, probes = %d, refreshes = %d", err, attempts.Load(), probes.Load(), refreshes.Load())
				}
			}
			stored, err := store.Get(authstore.UserGitHub)
			if err != nil {
				t.Fatal(err)
			}
			wantToken := record.Token
			if tc.source == authstore.SourceKeychain {
				wantToken = "ghu_fresh"
			}
			if stored.Token != wantToken || stored.Login != record.Login {
				t.Error("stored identity did not retain the expected login and token")
			}
		})
	}
}
