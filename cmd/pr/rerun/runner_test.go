package rerun

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/spf13/cobra"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func loggedIn(context.Context) (authstore.Token, error) {
	return authstore.Token{Value: "token_test", Login: "someone"}, nil
}

func pull(headRepo string) []sequence.Response {
	return []sequence.Response{{Body: map[string]any{
		"number": 7, "state": "open",
		"head": map[string]any{"sha": "abc", "ref": "feature", "repo": map[string]any{"full_name": headRepo}},
		"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "o/r"}},
	}}}
}

func pipelines(revision string) []sequence.Response {
	return []sequence.Response{{Body: map[string]any{"items": []any{
		map[string]any{"id": "p1", "number": 12, "state": "created", "vcs": map[string]any{"revision": revision}},
	}}}}
}

var failedPipeline = sequence.Routes{
	"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{
		map[string]any{"id": "w1", "name": "build", "status": "failed", "created_at": "2026-10-09T10:00:00Z"},
	}}}},
	"GET /api/v2/workflow/w1/job":    {{Body: map[string]any{"items": []any{map[string]any{"name": "push", "status": "failed"}}}}},
	"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w2"}}},
}

func start(t *testing.T, gh, cc sequence.Routes) (*runner, *githubmock.Server, *circlemock.Server) {
	t.Helper()
	ghServer, err := githubmock.Start(gh)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ghServer.Close)
	ccServer, err := circlemock.Start(cc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ccServer.Close)
	endpoints := agentcli.DefaultEndpoints()
	endpoints.GitHubAPIURL = ghServer.URL
	endpoints.CircleCIAPIURL = ccServer.APIURL()
	return &runner{
		gate:            func(bool) error { return nil },
		stdout:          &bytes.Buffer{},
		requireGitHub:   loggedIn,
		personGitHub:    loggedIn,
		requireCircleCI: loggedIn,
		endpoints:       func() agentcli.Endpoints { return endpoints },
	}, ghServer, ccServer
}

func execute(t *testing.T, r *runner, args ...string) (map[string]any, int) {
	t.Helper()
	err := r.Run(&cobra.Command{}, args)
	var doc map[string]any
	if err := json.Unmarshal(r.stdout.(*bytes.Buffer).Bytes(), &doc); err != nil {
		t.Fatalf("document: %v", err)
	}
	if int(doc["exitCode"].(float64)) != agentcli.Exit(err) {
		t.Errorf("exitCode %v, process exit %d", doc["exitCode"], agentcli.Exit(err))
	}
	return doc, agentcli.Exit(err)
}

// TestRerunsTheHeadPipeline: the head's pipeline on its branch is found and
// its failed workflow rerun from failed.
func TestRerunsTheHeadPipeline(t *testing.T) {
	r, _, cc := start(t,
		sequence.Routes{"GET /repos/o/r/pulls/7": pull("o/r")},
		mergeRoutes(failedPipeline, sequence.Routes{"GET /api/v2/project/gh/o/r/pipeline?branch=feature": pipelines("abc")}))
	doc, code := execute(t, r, "o/r", "7")
	if code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	workflows := doc["workflows"].([]any)
	if doc["headSha"] != "abc" || doc["identity"] != "app" || len(workflows) != 1 || workflows[0].(map[string]any)["rerunId"] != "w2" {
		t.Errorf("document: %v", doc)
	}
	if !posted(cc, "/api/v2/workflow/w1/rerun") {
		t.Errorf("no rerun posted: %v", cc.Requests())
	}
}

// TestForkReadsPullBranch: a head in a fork is built as pull/<number>.
func TestForkReadsPullBranch(t *testing.T) {
	r, _, cc := start(t,
		sequence.Routes{"GET /repos/o/r/pulls/7": pull("someone/r")},
		mergeRoutes(failedPipeline, sequence.Routes{"GET /api/v2/project/gh/o/r/pipeline?branch=pull%2F7": pipelines("abc")}))
	if doc, code := execute(t, r, "o/r", "7"); code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	if !posted(cc, "/api/v2/workflow/w1/rerun") {
		t.Errorf("no rerun posted")
	}
}

// TestStalledHeadPipelineRedeliversThePush: the head's pipeline pending
// without a workflow for an hour gets the push of the head revision sent
// again, with the identity that read the pull request.
func TestStalledHeadPipelineRedeliversThePush(t *testing.T) {
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	r, gh, _ := start(t,
		sequence.Routes{
			"GET /repos/o/r/pulls/7": pull("o/r"),
			"GET /repos/o/r/hooks":   {{Body: []any{map[string]any{"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"}}}}},
			"GET /repos/o/r/hooks/1/deliveries": {{Body: []any{
				map[string]any{"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "event": "push"},
			}}},
			"GET /repos/o/r/hooks/1/deliveries/42": {{Body: map[string]any{
				"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "event": "push",
				"request": map[string]any{"payload": map[string]any{"ref": "refs/heads/feature", "after": "abc"}},
			}}},
			"POST /repos/o/r/hooks/1/deliveries/42/attempts": {{Status: http.StatusAccepted}},
		},
		sequence.Routes{
			"GET /api/v2/project/gh/o/r/pipeline?branch=feature": {{Body: map[string]any{"items": []any{
				map[string]any{"id": "p1", "number": 12, "state": "pending", "created_at": created, "vcs": map[string]any{"revision": "abc"}},
			}}}},
			"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{}}}},
		})
	doc, code := execute(t, r, "o/r", "7")
	if code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	redelivery, _ := doc["redelivery"].(map[string]any)
	if doc["identity"] != "app" || redelivery["outcome"] != "redelivered" || redelivery["after"] != "abc" || redelivery["ref"] != "refs/heads/feature" {
		t.Errorf("document: %v", doc)
	}
	posted := false
	for _, req := range gh.Requests() {
		posted = posted || req.Method == http.MethodPost && req.Path == "/repos/o/r/hooks/1/deliveries/42/attempts"
	}
	if !posted {
		t.Errorf("no redelivery posted: %v", gh.Requests())
	}
}

// TestNoPipelineIsNotApplicable: CircleCI never built the head: exit 3.
func TestNoPipelineIsNotApplicable(t *testing.T) {
	r, _, _ := start(t,
		sequence.Routes{"GET /repos/o/r/pulls/7": pull("o/r")},
		sequence.Routes{"GET /api/v2/project/gh/o/r/pipeline?branch=feature": pipelines("other")})
	if doc, code := execute(t, r, "o/r", "7"); code != agentcli.ExitNotApplicable {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
}

// TestNoCircleCILogin: no CircleCI token is exit 8 before any CircleCI call.
func TestNoCircleCILogin(t *testing.T) {
	r, _, cc := start(t, sequence.Routes{"GET /repos/o/r/pulls/7": pull("o/r")}, sequence.Routes{})
	r.requireCircleCI = func(context.Context) (authstore.Token, error) {
		return authstore.Token{}, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "no token in the keychain", Hint: "devctl auth login --circleci-only"}
	}
	if doc, code := execute(t, r, "o/r", "7"); code != agentcli.ExitAuthRequired {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	if len(cc.Requests()) != 0 {
		t.Errorf("CircleCI was called: %v", cc.Requests())
	}
}

// TestUsage: a wrong call is exit 7 with a document.
func TestUsage(t *testing.T) {
	r, _, _ := start(t, sequence.Routes{}, sequence.Routes{})
	if doc, code := execute(t, r, "o/r"); code != agentcli.ExitUsage || doc["command"] != "pr rerun" {
		t.Fatalf("exit %d: %v", code, doc)
	}
}

func mergeRoutes(a, b sequence.Routes) sequence.Routes {
	out := sequence.Routes{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func posted(cc *circlemock.Server, path string) bool {
	for _, r := range cc.Requests() {
		if r.Method == http.MethodPost && r.Path == path {
			return true
		}
	}
	return false
}
