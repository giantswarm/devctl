package rerun

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authexec"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

type identityCase struct {
	owner, identity, source, token, hint string
}

var identityCases = []identityCase{
	{"giantswarm", authexec.IdentityApp, authstore.SourceKeychain, "app_test", "devctl GitHub App login"},
	{"outside", authexec.IdentityGH, authexec.SourceGHLogin, "gh_test", "your own gh login"},
}

// Unlike loggedIn, these sources are distinguishable both by their HTTP
// credentials and by the identity that the document must report.
func identitySources(r *runner, tc identityCase, selectedErr error) (selected, other *int) {
	selected, other = new(int), new(int)
	chosen := func(context.Context) (authstore.Token, error) {
		*selected += 1
		return authstore.Token{Value: tc.token, Source: tc.source}, selectedErr
	}
	unchosen := func(context.Context) (authstore.Token, error) {
		*other += 1
		return authstore.Token{Value: "wrong_test", Source: "wrong"}, nil
	}
	if tc.identity == authexec.IdentityApp {
		r.requireGitHub, r.personGitHub = chosen, unchosen
	} else {
		r.requireGitHub, r.personGitHub = unchosen, chosen
	}
	return selected, other
}

func identityPull(repository string) []sequence.Response {
	return []sequence.Response{{Body: map[string]any{
		"number": 7, "state": "open",
		"head": map[string]any{"sha": "abc", "ref": "feature", "repo": map[string]any{"full_name": repository}},
		"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": repository}},
	}}}
}

func TestGitHubIdentityIsKeptForRedelivery(t *testing.T) {
	for _, tc := range identityCases {
		t.Run(tc.identity, func(t *testing.T) {
			repository := tc.owner + "/r"
			prefix := "/repos/" + repository
			r, gh, cc := start(t, sequence.Routes{
				"GET " + prefix + "/pulls/7": identityPull(repository),
				"GET " + prefix + "/hooks": {{Body: []any{map[string]any{
					"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"},
				}}}},
				"GET " + prefix + "/hooks/1/deliveries": {{Body: []any{map[string]any{
					"id": 42, "guid": "g-push", "event": "push", "redelivery": false,
					"delivered_at": "2026-10-09T17:59:00Z",
				}}}},
				"GET " + prefix + "/hooks/1/deliveries/42": {{Body: map[string]any{
					"id": 42, "guid": "g-push", "event": "push", "redelivery": false,
					"delivered_at": "2026-10-09T17:59:00Z",
					"request":      map[string]any{"payload": map[string]any{"ref": "refs/heads/feature", "after": "abc"}},
				}}},
				"POST " + prefix + "/hooks/1/deliveries/42/attempts": {{Status: http.StatusAccepted}},
			}, sequence.Routes{
				"GET /api/v2/project/gh/" + repository + "/pipeline?branch=feature": {{Body: map[string]any{"items": []any{
					map[string]any{"id": "p1", "number": 12, "state": "pending",
						"created_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
						"vcs":        map[string]any{"revision": "abc"}},
				}}}, {Body: map[string]any{"items": []any{
					map[string]any{"id": "p2", "number": 13, "state": "created",
						"created_at": time.Now().UTC().Format(time.RFC3339),
						"vcs":        map[string]any{"revision": "abc"}},
				}}}},
				"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{}}}},
			})
			selected, other := identitySources(r, tc, nil)
			doc, code := execute(t, r, repository, "7")
			if code != agentcli.ExitOK || doc["identity"] != tc.identity || doc["repository"] != repository {
				t.Fatalf("exit %d, document %v", code, doc)
			}
			if *selected != 1 || *other != 0 {
				t.Errorf("login lookups: selected %d, other %d; want 1, 0", *selected, *other)
			}
			requests := gh.Requests()
			if len(requests) != 5 || requests[4].Method != http.MethodPost || requests[4].Path != prefix+"/hooks/1/deliveries/42/attempts" {
				t.Fatalf("expected PR read and one webhook redelivery, got %v", requests)
			}
			for _, req := range requests {
				if req.Header.Get("Authorization") != "Bearer "+tc.token {
					t.Errorf("%s used the wrong GitHub identity", req.String())
				}
				if req.Header.Get("Circle-Token") != "" {
					t.Error("CircleCI credential sent to GitHub")
				}
			}
			if redelivery := doc["redelivery"].(map[string]any); redelivery["outcome"] != "redelivered" || redelivery["after"] != "abc" {
				t.Errorf("redelivery: %v", redelivery)
			}
			if len(cc.Requests()) != 2 {
				t.Fatalf("expected pipeline and workflows reads, got %v", cc.Requests())
			}
			for _, req := range cc.Requests() {
				if req.Header.Get("Circle-Token") != "token_test" || req.Header.Get("Authorization") != "" {
					t.Errorf("%s used the wrong service credential", req.String())
				}
			}
		})
	}
}

func TestGitHubIdentityFailureStopsBeforeCircleCI(t *testing.T) {
	for _, tc := range identityCases {
		t.Run(tc.identity, func(t *testing.T) {
			r, gh, cc := start(t, sequence.Routes{}, sequence.Routes{})
			hint := "gh auth login"
			if tc.identity == authexec.IdentityApp {
				hint = "devctl auth login --github-only"
			}
			selected, other := identitySources(r, tc, &authstore.AuthRequiredError{
				Identity: "GitHub", Cause: "synthetic missing login", Hint: hint,
			})
			circleCILogins := 0
			r.requireCircleCI = func(context.Context) (authstore.Token, error) {
				circleCILogins++
				return loggedIn(context.Background())
			}
			doc, code := execute(t, r, tc.owner+"/r", "7")
			if code != agentcli.ExitAuthRequired || doc["verdict"] != string(agentcli.VerdictAuthRequired) ||
				doc["identity"] != "" || !strings.Contains(doc["reason"].(string), hint) {
				t.Errorf("exit %d, document %v", code, doc)
			}
			if *selected != 1 || *other != 0 || circleCILogins != 0 {
				t.Errorf("login lookups: selected %d, other %d, CircleCI %d; want 1, 0, 0", *selected, *other, circleCILogins)
			}
			if len(gh.Requests()) != 0 || len(cc.Requests()) != 0 {
				t.Error("API work continued after the selected login failed")
			}
		})
	}
}

func TestGitHubNotFoundExplainsSelectedIdentity(t *testing.T) {
	for _, tc := range identityCases {
		t.Run(tc.identity, func(t *testing.T) {
			repository := tc.owner + "/r"
			path := "/repos/" + repository + "/pulls/7"
			r, gh, cc := start(t, sequence.Routes{
				"GET " + path: {{Status: http.StatusNotFound, Body: map[string]any{"message": "Not Found"}}},
			}, sequence.Routes{})
			selected, other := identitySources(r, tc, nil)
			circleCILogins := 0
			r.requireCircleCI = func(context.Context) (authstore.Token, error) {
				circleCILogins++
				return loggedIn(context.Background())
			}
			doc, code := execute(t, r, repository, "7")
			if code != agentcli.ExitUsage || doc["verdict"] != string(agentcli.VerdictUsage) || doc["identity"] != tc.identity ||
				!strings.Contains(doc["reason"].(string), tc.hint) || doc["repository"] != repository || doc["number"] != float64(7) {
				t.Errorf("exit %d, document %v", code, doc)
			}
			if *selected != 1 || *other != 0 || circleCILogins != 0 || len(cc.Requests()) != 0 {
				t.Errorf("work continued after 404: login lookups selected %d, other %d, CircleCI %d; CircleCI requests %d",
					*selected, *other, circleCILogins, len(cc.Requests()))
			}
			requests := gh.Requests()
			if len(requests) != 1 || requests[0].Path != path || requests[0].Header.Get("Authorization") != "Bearer "+tc.token {
				t.Error("404 read did not use only the selected identity")
			}
		})
	}
}
