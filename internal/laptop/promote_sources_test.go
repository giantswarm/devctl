package laptop

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/releasepromote"
)

func TestReleasePromoteSourcesKeepsTheSelectedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		ci             bool
		override       bool
		acceptOverride bool
		wantTokens     []string
		wantRefreshes  int
		wantError      bool
	}{
		{name: "stored App login renews", wantTokens: []string{"Bearer test-app-stale", "Bearer test-app-fresh"}, wantRefreshes: 1},
		{name: "rejected override cannot switch to App login", override: true, wantTokens: []string{"Bearer test-override"}, wantError: true},
		{name: "accepted override has priority", override: true, acceptOverride: true, wantTokens: []string{"Bearer test-override"}},
		{name: "CI rejected override cannot switch to App login", ci: true, override: true, wantTokens: []string{"Bearer test-override"}, wantError: true},
		{name: "CI cannot use an unselected stored login", ci: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range authstore.GitHubEnvVars {
				t.Setenv(name, "")
			}
			t.Setenv(authstore.EnvCI, "")
			if tc.ci {
				t.Setenv(authstore.EnvCI, "true")
			}
			if tc.override {
				t.Setenv("DEVCTL_GITHUB_TOKEN", "test-override")
				t.Setenv("GITHUB_TOKEN", "test-lower-priority")
				t.Setenv("OPSCTL_GITHUB_TOKEN", "test-lowest-priority")
			}
			store := &authstore.FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
			t.Setenv(agentcli.EnvKeyringFile, store.Path)
			original := authstore.Record{Login: "fixture-person", Token: "test-app-stale", RefreshToken: "test-app-refresh"} // #nosec G101 -- fixture value
			if err := store.Set(authstore.UserGitHub, original); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var tokens []string
			refreshes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repos/giantswarm/fixture"):
					token := r.Header.Get("Authorization")
					tokens = append(tokens, token)
					if token != "Bearer test-app-fresh" && (!tc.acceptOverride || token != "Bearer test-override") { // #nosec G101 -- fixture value
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = w.Write([]byte(`{"message":"synthetic token rejection"}`))
						return
					}
					_, _ = w.Write([]byte(`{"default_branch":"main"}`))
				case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
					refreshes++
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != original.RefreshToken {
						t.Error("renewal did not use the selected App login's refresh grant")
					}
					_, _ = w.Write([]byte(`{"access_token":"test-app-fresh","token_type":"bearer","expires_in":3600,"refresh_token":"test-app-next-refresh","refresh_token_expires_in":7200}`))
				default:
					t.Errorf("unexpected fixture request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			t.Setenv(agentcli.EnvGitHubAPIURL, server.URL)
			t.Setenv(agentcli.EnvGitHubOAuthURL, server.URL)
			var warnings []string
			sources, err := ReleasePromoteSources(t.Context(), agentcli.EndpointsFromEnv(), server.Client().Transport, func(warning string) {
				if warning != "" {
					warnings = append(warnings, warning)
				}
			})
			if tc.ci && !tc.override {
				if sources != nil || agentcli.Exit(err) != agentcli.ExitAuthRequired {
					t.Fatalf("CI must refuse the stored login without an override: sources=%v, err=%v", sources, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				identity := releasepromote.Identity{Source: authstore.SourceKeychain, Login: original.Login}
				if tc.override {
					identity = releasepromote.Identity{Source: "$DEVCTL_GITHUB_TOKEN"}
				}
				if sources.Identity != identity || (sources.NotFoundHint != "") != !tc.override {
					t.Errorf("identity and access hint must describe the selected credential source: identity=%v, hint=%q", sources.Identity, sources.NotFoundHint)
				}
				branch, err := sources.GitHub.DefaultBranch(t.Context(), "giantswarm", "fixture")
				if (err != nil) != tc.wantError || (!tc.wantError && branch != "main") {
					t.Errorf("unexpected lookup outcome: branch=%q, err=%v", branch, err)
				}
			}
			mu.Lock()
			if !slices.Equal(tokens, tc.wantTokens) || refreshes != tc.wantRefreshes {
				t.Errorf("requests changed identity or renewed unexpectedly: tokens=%v, refreshes=%d", tokens, refreshes)
			}
			mu.Unlock()
			if tc.override && !tc.ci {
				if len(warnings) != 1 || !strings.Contains(warnings[0], "$DEVCTL_GITHUB_TOKEN") || strings.Contains(warnings[0], "test-override") {
					t.Errorf("expected one source warning without token material, got %v", warnings)
				}
			} else if len(warnings) != 0 {
				t.Errorf("unexpected source warnings: %v", warnings)
			}
			record, err := store.Get(authstore.UserGitHub)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantRefreshes == 0 && record != original {
				t.Error("an override or CI refusal must not modify the stored login")
			}
			if tc.wantRefreshes == 1 && (record.Login != original.Login || record.Token != "test-app-fresh" || record.RefreshToken != "test-app-next-refresh") {
				t.Error("renewal must persist the new credentials for the same login")
			}
		})
	}
}
