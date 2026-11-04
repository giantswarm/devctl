package deploy

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

type renewalTransportFunc func(*http.Request) (*http.Response, error)

func (f renewalTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func renewalResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Request: req, StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func TestDeployGitHubRenewal(t *testing.T) {
	for _, tc := range []struct {
		name             string
		alreadyRenewed   bool
		refuseRefresh    bool
		refuseRenewed    bool
		override         bool
		wantReads        int
		wantRefreshes    int
		wantAuthRequired bool
		wantUnauthorized bool
	}{
		{name: "refresh expired access", wantReads: 2, wantRefreshes: 1},
		{name: "pick up another process renewal", alreadyRenewed: true, wantReads: 2},
		{name: "refused refresh", refuseRefresh: true, wantReads: 1, wantRefreshes: 1, wantAuthRequired: true},
		{name: "renewed access refused once", refuseRenewed: true, wantReads: 2, wantRefreshes: 1, wantUnauthorized: true},
		{name: "explicit override never falls back", override: true, wantReads: 1, wantUnauthorized: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"DEVCTL_GITHUB_TOKEN", "GITHUB_TOKEN", "OPSCTL_GITHUB_TOKEN"} {
				t.Setenv(name, "")
			}
			t.Setenv("CI", "")
			t.Setenv(agentcli.EnvTimeScale, "1")
			path := filepath.Join(t.TempDir(), "keyring.json")
			t.Setenv(agentcli.EnvKeyringFile, path)
			t.Setenv(agentcli.EnvGitHubOAuthURL, "http://127.0.0.1:1")
			store := &authstore.FileStore{Path: path}
			// These values exist only in this offline fixture.
			stale := authstore.Record{
				Login: "fixture-user", Token: "synthetic-old-access", // #nosec G101 -- synthetic offline fixture
				ExpiresAt:    time.Now().Add(time.Minute),
				RefreshToken: "synthetic-old-refresh", RefreshExpiresAt: time.Now().Add(time.Hour), // #nosec G101 -- synthetic offline fixture
			}
			const freshAccess = "synthetic-new-access"
			const freshRefresh = "synthetic-new-refresh"
			if tc.override {
				t.Setenv("CI", "1")
				t.Setenv("DEVCTL_GITHUB_TOKEN", "synthetic-explicit-override")
				// A broken fallback must fail rather than silently read a login.
				require.NoError(t, os.WriteFile(path, []byte("fixture store must not be read"), 0o600))
			} else {
				require.NoError(t, store.Set(authstore.UserGitHub, stale))
			}
			token, err := authstore.ResolveGitHub(t.Context())
			require.NoError(t, err)
			if tc.alreadyRenewed {
				newer := stale
				newer.Token, newer.RefreshToken = freshAccess, freshRefresh
				newer.ExpiresAt = time.Now().Add(time.Hour)
				require.NoError(t, store.Set(authstore.UserGitHub, newer))
			}
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			reads, refreshes := 0, 0
			// This is the only transport: unexpected destinations never open a
			// socket, so no real GitHub, keychain or credential is involved.
			http.DefaultTransport = renewalTransportFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.String() {
				case "https://api.github.com/repos/fixture-owner/fixture-gitops/pulls/42":
					reads++
					require.Equal(t, http.MethodGet, req.Method)
					if reads == 1 {
						require.True(t, req.Header.Get("Authorization") == "Bearer "+token.Value, "first read must carry the resolved token")
					}
					if reads == 1 || tc.refuseRenewed || tc.override {
						return renewalResponse(req, http.StatusUnauthorized, `{"message":"Bad credentials"}`), nil
					}
					require.True(t, req.Header.Get("Authorization") == "Bearer "+freshAccess, "retry must carry the renewed token")
					return renewalResponse(req, http.StatusOK, `{"number":42,"merged":true}`), nil
				case "http://127.0.0.1:1/login/oauth/access_token":
					refreshes++
					require.Equal(t, http.MethodPost, req.Method)
					require.NoError(t, req.ParseForm())
					require.Equal(t, "refresh_token", req.Form.Get("grant_type"))
					require.True(t, req.Form.Get("refresh_token") == stale.RefreshToken, "refresh must use the stored synthetic token")
					if tc.refuseRefresh {
						return renewalResponse(req, http.StatusOK, `{"error":"invalid_grant"}`), nil
					}
					return renewalResponse(req, http.StatusOK, `{"access_token":"synthetic-new-access","refresh_token":"synthetic-new-refresh","expires_in":3600,"refresh_token_expires_in":3600}`), nil
				default:
					t.Error("unexpected offline request")
					return nil, errors.New("unexpected offline request")
				}
			})
			client, err := newGitHubClient(token)
			require.NoError(t, err)
			err = client.WaitForPRMerge(t.Context(), "fixture-owner", "fixture-gitops", 42, time.Second)
			switch {
			case tc.wantAuthRequired:
				require.ErrorIs(t, err, authstore.ErrAuthRequired)
			case tc.wantUnauthorized:
				var answer *github.ErrorResponse
				require.ErrorAs(t, err, &answer)
				require.Equal(t, http.StatusUnauthorized, answer.Response.StatusCode)
			default:
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantReads, reads)
			require.Equal(t, tc.wantRefreshes, refreshes)
			if tc.override {
				data, err := os.ReadFile(path) // #nosec G304 -- path is the fixed fixture name inside t.TempDir, never user input
				require.NoError(t, err)
				require.Equal(t, "fixture store must not be read", string(data))
				return
			}
			got, err := store.Get(authstore.UserGitHub)
			require.NoError(t, err)
			require.Equal(t, stale.Login, got.Login)
			if tc.refuseRefresh {
				require.True(t, got.Token == stale.Token && got.RefreshToken == stale.RefreshToken)
			} else {
				require.True(t, got.Token == freshAccess && got.RefreshToken == freshRefresh)
				require.True(t, got.ExpiresAt.After(time.Now().Add(30*time.Minute)))
			}
		})
	}
}
