package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func loggedIn(context.Context) (authstore.Token, error) {
	return authstore.Token{Value: "token_test", Login: "someone"}, nil
}

var pipeline = sequence.Routes{
	"GET /api/v2/project/gh/o/r/pipeline/12": {{Body: map[string]any{"id": "p1", "number": 12, "state": "created", "project_slug": "gh/o/r", "vcs": map[string]any{"branch": "main", "revision": "abc"}}}},
	"GET /api/v2/pipeline/p1/workflow":       {{Body: map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": "success", "created_at": "2026-10-09T10:00:00Z", "stopped_at": "2026-10-09T10:05:00Z"}}}}},
	"GET /api/v2/workflow/w1/job":            {{Body: map[string]any{"items": []any{map[string]any{"name": "lint", "type": "build", "status": "success", "job_number": 1, "started_at": "2026-10-09T10:00:10Z", "stopped_at": "2026-10-09T10:04:50Z"}}}}},
}

func start(t *testing.T, cc sequence.Routes) (*runner, *circlemock.Server) {
	t.Helper()
	ccServer, err := circlemock.Start(cc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ccServer.Close)
	endpoints := agentcli.DefaultEndpoints()
	endpoints.CircleCIAPIURL = ccServer.APIURL()
	return &runner{
		gate:            func(bool) error { return nil },
		stdout:          &bytes.Buffer{},
		requireCircleCI: loggedIn,
		endpoints:       func() agentcli.Endpoints { return endpoints },
		clock:           agentcli.SystemClock,
	}, ccServer
}

func execute(t *testing.T, r *runner, args ...string) (map[string]any, int) {
	t.Helper()
	// One document per run: the buffer holds the last one only.
	r.stdout.(*bytes.Buffer).Reset()
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

// TestReadsThePipeline: the pipeline is read with the keychain login and
// reported as listed.
func TestReadsThePipeline(t *testing.T) {
	r, cc := start(t, pipeline)
	doc, code := execute(t, r, "o/r", "12")
	if code != agentcli.ExitOK || doc["verdict"] != "listed" || doc["command"] != "ci jobs" || doc["repository"] != "o/r" {
		t.Fatalf("exit %d: %v", code, doc)
	}
	workflows := doc["workflows"].([]any)
	if len(workflows) != 1 || workflows[0].(map[string]any)["latest"] != true || len(workflows[0].(map[string]any)["jobs"].([]any)) != 1 {
		t.Errorf("workflows: %v", workflows)
	}
	if doc["pipeline"].(map[string]any)["number"] != float64(12) {
		t.Errorf("pipeline: %v", doc["pipeline"])
	}
	for _, req := range cc.Requests() {
		if req.Header.Get("Circle-Token") != "token_test" {
			t.Errorf("a request without the login's token: %v", req)
		}
	}
}

// TestNoCircleCILogin: no CircleCI token is exit 8 before any CircleCI call.
func TestNoCircleCILogin(t *testing.T) {
	r, cc := start(t, pipeline)
	r.requireCircleCI = func(context.Context) (authstore.Token, error) {
		return authstore.Token{}, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "no token in the keychain", Hint: "devctl auth login --circleci-only"}
	}
	if doc, code := execute(t, r, "o/r", "12"); code != agentcli.ExitAuthRequired {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	if len(cc.Requests()) != 0 {
		t.Errorf("CircleCI was called: %v", cc.Requests())
	}
}

// TestUsage: a wrong call is exit 7 with a document.
func TestUsage(t *testing.T) {
	r, _ := start(t, sequence.Routes{})
	if doc, code := execute(t, r, "o/r"); code != agentcli.ExitUsage || doc["command"] != "ci jobs" {
		t.Fatalf("exit %d: %v", code, doc)
	}
	if doc, code := execute(t, r, "o", "12"); code != agentcli.ExitUsage || doc["verdict"] != "usage" {
		t.Fatalf("exit %d: %v", code, doc)
	}
}
