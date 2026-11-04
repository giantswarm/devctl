package login

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestLoginCircleCIFlows(t *testing.T) {
	const deviceURL = "https://device.example.invalid/login"
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	oldGitHub := authstore.Record{
		Login: "previous-engineer", Token: "fixture-previous-github", RefreshToken: "fixture-previous-refresh",
		ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	cases := []struct {
		name           string
		circleCIOnly   bool
		previousGitHub bool
	}{
		{name: "case 0: default login completes both flows"},
		{name: "case 1: CircleCI only with GitHub absent", circleCIOnly: true},
		{name: "case 2: CircleCI only preserves GitHub", circleCIOnly: true, previousGitHub: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gh, err := github.Start(sequence.Routes{
				"POST /login/device/code": {{Body: map[string]any{
					"device_code": "fixture-device", "user_code": "ABCD-1234", "verification_uri": deviceURL, "expires_in": 900, "interval": 1,
				}}},
				"POST /login/oauth/access_token": {{Body: map[string]any{ //nolint:gosec // G101: synthetic tokens issued only by the local OAuth mock
					"access_token": "fixture-github-access", "refresh_token": "fixture-github-refresh", "expires_in": 28800, "refresh_token_expires_in": 15897600,
				}}},
				"GET /user": {{Body: map[string]any{"login": "github-engineer"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer gh.Close()
			cci, err := circleci.Start(sequence.Routes{
				"POST /oauth/register": {{Status: http.StatusCreated, Body: map[string]any{"client_id": "fixture-client"}}},
				"POST /oauth/token":    {{Body: map[string]any{"access_token": "fixture-circleci-access", "expires_in": 7776000}}},
				"GET /api/v2/me":       {{Body: map[string]any{"login": "circleci-engineer"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer cci.Close()
			callbackClient := &http.Client{Timeout: 2 * time.Second}
			defer callbackClient.CloseIdleConnections()
			var githubConsents, circleCIConsents int
			opener := func(raw string) error {
				if raw == deviceURL {
					githubConsents++
					return nil
				}
				u, err := url.Parse(raw)
				if err != nil || !strings.HasPrefix(raw, cci.URL+"/oauth/authorize?") {
					return fmt.Errorf("unexpected browser URL %q", raw)
				}
				circleCIConsents++
				callback, err := url.Parse(u.Query().Get("redirect_uri"))
				if err != nil || callback.Scheme != "http" || callback.Hostname() != "127.0.0.1" || callback.Path != "/callback" {
					return errors.New("the callback must stay on the test's loopback listener")
				}
				callback.RawQuery = url.Values{"state": {u.Query().Get("state")}, "code": {"fixture-code"}}.Encode()
				resp, err := callbackClient.Get(callback.String())
				if err != nil {
					return err
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("callback: HTTP %d", resp.StatusCode)
				}
				return nil
			}
			r, store, stdout, stderr := newRunnerWithBrowser(t, nil, opener)
			r.flag.CircleCIOnly = tc.circleCIOnly
			r.clock = agentcli.NewClock(0.001, func() time.Time { return now })
			r.open = func(w io.Writer) (*authstore.Auth, error) {
				return authstore.New(authstore.Config{
					Store: store, Clock: r.clock, OpenBrowser: opener, Stderr: w,
					Endpoints: agentcli.Endpoints{
						GitHubOAuthURL: gh.URL, GitHubAPIURL: gh.URL,
						CircleCIOAuthURL: cci.URL, CircleCIAPIURL: cci.APIURL(),
					},
				})
			}
			if tc.previousGitHub {
				if err := store.Set(authstore.UserGitHub, oldGitHub); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.run(context.Background(), nil); err != nil {
				t.Fatalf("login: %v", err)
			}
			var doc document
			if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
				t.Fatalf("stdout is not one JSON document: %v", err)
			}
			if doc.Command != command || doc.ExitCode != agentcli.ExitOK || doc.Verdict != agentcli.VerdictGreen || doc.Reason != "" {
				t.Fatalf("envelope = %+v", doc.Envelope)
			}
			if !doc.CircleCI.Present || doc.CircleCI.Login != "circleci-engineer" || doc.CircleCI.Expired || doc.CircleCI.ExpiresAt == nil || !doc.CircleCI.ExpiresAt.Equal(now.Add(90*24*time.Hour)) {
				t.Fatalf("CircleCI identity = %+v", doc.CircleCI)
			}
			if doc.Manager != nil || doc.Muster.Present {
				t.Fatal("a GitHub/CircleCI login reported a muster sign-in")
			}
			circleCIRecord, err := store.Get(authstore.UserCircleCI)
			if err != nil || circleCIRecord.Token != "fixture-circleci-access" || circleCIRecord.Login != doc.CircleCI.Login || circleCIRecord.ClientID != "fixture-client" {
				t.Fatalf("CircleCI record was not stored correctly: %v", err)
			}
			githubRecord, err := store.Get(authstore.UserGitHub)
			if tc.circleCIOnly {
				if githubConsents != 0 || len(gh.Requests()) != 0 {
					t.Fatal("CircleCI-only started a GitHub flow")
				}
				if tc.previousGitHub {
					if err != nil || githubRecord != oldGitHub || !doc.GitHub.Present || doc.GitHub.Login != oldGitHub.Login {
						t.Fatal("CircleCI-only did not preserve and report the existing GitHub identity")
					}
				} else if !errors.Is(err, authstore.ErrNotFound) || doc.GitHub.Present {
					t.Fatal("CircleCI-only stored or reported an absent GitHub identity")
				}
			} else if githubConsents != 1 || len(gh.Requests()) != 3 || err != nil || githubRecord.Token != "fixture-github-access" || githubRecord.RefreshToken != "fixture-github-refresh" || !doc.GitHub.Present || doc.GitHub.Login != "github-engineer" || !doc.GitHub.Refreshable {
				t.Fatal("the default login did not complete and report GitHub authorization")
			}
			if circleCIConsents != 1 || len(cci.Requests()) != 3 {
				t.Fatal("the CircleCI authorization was not completed exactly once")
			}
			for _, token := range []string{"fixture-github-access", "fixture-github-refresh", "fixture-circleci-access", oldGitHub.Token, oldGitHub.RefreshToken} {
				if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
					t.Fatal("output contains a synthetic access or refresh token")
				}
			}
		})
	}
}
