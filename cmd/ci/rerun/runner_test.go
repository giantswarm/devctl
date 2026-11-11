package rerun

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
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

var failedWorkflow = map[string]any{"id": "w1", "name": "build", "status": "failed", "created_at": "2026-10-09T10:00:00Z", "stopped_at": "2026-10-09T10:05:00Z", "pipeline_id": "p1", "pipeline_number": 12, "project_slug": "gh/o/r"}

var failed = sequence.Routes{
	"GET /api/v2/workflow/w1":          {{Body: failedWorkflow}},
	"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{failedWorkflow}}}},
	"POST /api/v2/workflow/w1/rerun":   {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w2"}}},
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
		flag:            &flag{},
		stdout:          &bytes.Buffer{},
		requireCircleCI: loggedIn,
		endpoints:       func() agentcli.Endpoints { return endpoints },
		clock:           agentcli.SystemClock,
	}, ccServer
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

func posted(cc *circlemock.Server, path string) bool {
	for _, r := range cc.Requests() {
		if r.Method == http.MethodPost && r.Path == path {
			return true
		}
	}
	return false
}

// TestRerunsTheWorkflow: the workflow is rerun with the flags as given and
// the document carries the new run.
func TestRerunsTheWorkflow(t *testing.T) {
	r, cc := start(t, failed)
	r.flag.FromFailed = true
	doc, code := execute(t, r, "o/r", "w1")
	if code != agentcli.ExitOK || doc["command"] != "ci rerun" || doc["verdict"] != "green" {
		t.Fatalf("exit %d: %v", code, doc)
	}
	if doc["outcome"] != "rerun" || doc["rerunId"] != "w2" || doc["fromFailed"] != true || doc["canceled"] != false || doc["repository"] != "o/r" {
		t.Errorf("document: %v", doc)
	}
	if !posted(cc, "/api/v2/workflow/w1/rerun") {
		t.Errorf("no rerun posted: %v", cc.Requests())
	}
}

// TestNoCircleCILogin: no CircleCI token is exit 8 before any CircleCI call.
func TestNoCircleCILogin(t *testing.T) {
	r, cc := start(t, failed)
	r.requireCircleCI = func(context.Context) (authstore.Token, error) {
		return authstore.Token{}, &authstore.AuthRequiredError{Identity: "CircleCI", Cause: "no token in the keychain", Hint: "devctl auth login --circleci-only"}
	}
	if doc, code := execute(t, r, "o/r", "w1"); code != agentcli.ExitAuthRequired {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	if len(cc.Requests()) != 0 {
		t.Errorf("CircleCI was called: %v", cc.Requests())
	}
}

// TestUsage: a wrong call is exit 7 with a document.
func TestUsage(t *testing.T) {
	r, _ := start(t, sequence.Routes{})
	if doc, code := execute(t, r, "o/r"); code != agentcli.ExitUsage || doc["command"] != "ci rerun" {
		t.Fatalf("exit %d: %v", code, doc)
	}
}
