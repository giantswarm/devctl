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

func run(t *testing.T, gh, cc sequence.Routes, args ...string) (map[string]any, int, *githubmock.Server, *circlemock.Server) {
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
	stdout := &bytes.Buffer{}
	r := &runner{
		gate:            func(bool) error { return nil },
		stdout:          stdout,
		requireGitHub:   loggedIn,
		personGitHub:    loggedIn,
		requireCircleCI: loggedIn,
		endpoints:       func() agentcli.Endpoints { return endpoints },
	}
	err = r.Run(&cobra.Command{}, args)
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("document: %v", err)
	}
	return doc, agentcli.Exit(err), ghServer, ccServer
}

var tagPipeline = sequence.Routes{
	"GET /api/v2/project/gh/o/r/pipeline": {{Body: map[string]any{"items": []any{
		map[string]any{"id": "p2", "number": 13, "vcs": map[string]any{"tag": "v1.2.4"}},
		map[string]any{"id": "p1", "number": 12, "vcs": map[string]any{"tag": "v1.2.3", "revision": "abc"}},
	}}}},
	"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{
		map[string]any{"id": "w1", "name": "release", "status": "failed", "created_at": "2026-10-09T10:00:00Z"},
	}}}},
	"GET /api/v2/workflow/w1/job":    {{Body: map[string]any{"items": []any{map[string]any{"name": "push-chart", "status": "failed"}}}}},
	"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w2"}}},
}

// TestRerunsTheTagPipeline: a version without its v finds the tag vX.Y.Z,
// and the tag's failed workflow is rerun from failed; GitHub is not read.
func TestRerunsTheTagPipeline(t *testing.T) {
	doc, code, gh, _ := run(t, sequence.Routes{}, tagPipeline, "o/r", "1.2.3")
	if code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	workflows := doc["workflows"].([]any)
	if doc["tag"] != "v1.2.3" || doc["headSha"] != "abc" || len(workflows) != 1 || workflows[0].(map[string]any)["rerunId"] != "w2" {
		t.Errorf("document: %v", doc)
	}
	if _, read := doc["identity"]; read || len(gh.Requests()) != 0 {
		t.Errorf("GitHub was read: %v %v", doc["identity"], gh.Requests())
	}
}

// TestStalledTagPipelineRedeliversThePush: the tag's pipeline pending
// without a workflow for an hour gets the tag's push delivery sent again, as
// the App for a giantswarm repository.
func TestStalledTagPipelineRedeliversThePush(t *testing.T) {
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	cc := sequence.Routes{
		"GET /api/v2/project/gh/giantswarm/r/pipeline": {{Body: map[string]any{"items": []any{
			map[string]any{"id": "p1", "number": 12, "state": "pending", "created_at": created, "vcs": map[string]any{"tag": "v1.2.3", "revision": "abc"}},
		}}}},
		"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{}}}},
	}
	gh := sequence.Routes{
		"GET /repos/giantswarm/r/hooks": {{Body: []any{map[string]any{"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"}}}}},
		"GET /repos/giantswarm/r/hooks/1/deliveries": {{Body: []any{
			map[string]any{"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "event": "push"},
		}}},
		"GET /repos/giantswarm/r/hooks/1/deliveries/42": {{Body: map[string]any{
			"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "event": "push",
			"request": map[string]any{"payload": map[string]any{"ref": "refs/tags/v1.2.3", "after": "abc"}},
		}}},
		"POST /repos/giantswarm/r/hooks/1/deliveries/42/attempts": {{Status: http.StatusAccepted}},
	}
	doc, code, ghServer, _ := run(t, gh, cc, "giantswarm/r", "v1.2.3")
	if code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	redelivery, _ := doc["redelivery"].(map[string]any)
	if doc["identity"] != "app" || redelivery["outcome"] != "redelivered" || redelivery["guid"] != "g-push" || len(doc["warnings"].([]any)) != 1 {
		t.Errorf("document: %v", doc)
	}
	posted := false
	for _, r := range ghServer.Requests() {
		posted = posted || r.Method == http.MethodPost && r.Path == "/repos/giantswarm/r/hooks/1/deliveries/42/attempts"
	}
	if !posted {
		t.Errorf("no redelivery posted: %v", ghServer.Requests())
	}
}

// erroredTagPipeline is the tag's pipeline that errored before any workflow
// an hour ago, created by trigger, and its push delivery already sent again.
func erroredTagPipeline(trigger string) (gh, cc sequence.Routes) {
	created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	cc = sequence.Routes{
		"GET /api/v2/project/gh/giantswarm/r/pipeline": {{Body: map[string]any{"items": []any{
			map[string]any{"id": "p1", "number": 12, "state": "errored", "created_at": created, "trigger": map[string]any{"type": trigger},
				"vcs": map[string]any{"tag": "v1.2.3", "revision": "abc"}},
		}}}},
		"GET /api/v2/pipeline/p1/workflow":              {{Body: map[string]any{"items": []any{}}}},
		"POST /api/v2/project/gh/giantswarm/r/pipeline": {{Status: http.StatusCreated, Body: map[string]any{"id": "p2", "number": 13, "state": "pending"}}},
	}
	gh = sequence.Routes{
		"GET /repos/giantswarm/r/hooks": {{Body: []any{map[string]any{"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"}}}}},
		"GET /repos/giantswarm/r/hooks/1/deliveries": {{Body: []any{
			map[string]any{"id": 43, "guid": "g-push", "delivered_at": "2026-10-09T18:10:00Z", "redelivery": true, "event": "push"},
			map[string]any{"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "event": "push"},
		}}},
		"GET /repos/giantswarm/r/hooks/1/deliveries/42": {{Body: map[string]any{
			"id": 42, "guid": "g-push", "delivered_at": "2026-10-09T17:59:00Z", "redelivery": false, "event": "push",
			"request": map[string]any{"payload": map[string]any{"ref": "refs/tags/v1.2.3", "after": "abc"}},
		}}},
	}
	return gh, cc
}

func circleCIPosts(server *circlemock.Server) []string {
	var out []string
	for _, r := range server.Requests() {
		if r.Method == http.MethodPost {
			out = append(out, r.Path)
		}
	}
	return out
}

// TestErroredTagPipelineTriggersAFreshOne: the tag's pipeline errored before
// any workflow and its push was already sent again: a fresh pipeline for the
// tag is triggered through CircleCI's API, no second redelivery sent.
func TestErroredTagPipelineTriggersAFreshOne(t *testing.T) {
	gh, cc := erroredTagPipeline("webhook")
	doc, code, ghServer, ccServer := run(t, gh, cc, "giantswarm/r", "v1.2.3")
	if code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	redelivery, _ := doc["redelivery"].(map[string]any)
	trigger, _ := doc["trigger"].(map[string]any)
	pipeline, _ := trigger["pipeline"].(map[string]any)
	if redelivery["outcome"] != "already_redelivered" || trigger["outcome"] != "triggered" || pipeline["number"] != float64(13) || len(doc["warnings"].([]any)) != 1 {
		t.Errorf("document: %v", doc)
	}
	if got := circleCIPosts(ccServer); len(got) != 1 || got[0] != "/api/v2/project/gh/giantswarm/r/pipeline" {
		t.Errorf("CircleCI posts: %q", got)
	}
	for _, r := range ghServer.Requests() {
		if r.Method == http.MethodPost {
			t.Errorf("GitHub posted %s", r.Path)
		}
	}
}

// TestFreshTagPipelineIsTriggeredOnce: a pipeline without a workflow that
// the API triggered itself gets no second one: exit 5, nothing posted.
func TestFreshTagPipelineIsTriggeredOnce(t *testing.T) {
	gh, cc := erroredTagPipeline("api")
	doc, code, _, ccServer := run(t, gh, cc, "giantswarm/r", "v1.2.3")
	if code != agentcli.ExitRefused {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	if trigger, _ := doc["trigger"].(map[string]any); trigger["outcome"] != "already_triggered" {
		t.Errorf("document: %v", doc)
	}
	if got := circleCIPosts(ccServer); len(got) != 0 {
		t.Errorf("CircleCI posts: %q", got)
	}
}

// TestUnknownTag: a tag among no recent pipeline is exit 3, nothing posted.
func TestUnknownTag(t *testing.T) {
	doc, code, _, cc := run(t, sequence.Routes{}, tagPipeline, "o/r", "v9.9.9")
	if code != agentcli.ExitNotApplicable {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	for _, r := range cc.Requests() {
		if r.Method == http.MethodPost {
			t.Errorf("posted %s", r.Path)
		}
	}
}

// TestReleaseUsage: a wrong call is exit 7 with a document.
func TestReleaseUsage(t *testing.T) {
	if doc, code, _, _ := run(t, sequence.Routes{}, sequence.Routes{}, "o/r"); code != agentcli.ExitUsage || doc["command"] != "release rerun" {
		t.Fatalf("exit %d: %v", code, doc)
	}
}
