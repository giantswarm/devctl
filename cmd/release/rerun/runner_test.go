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

func run(t *testing.T, routes sequence.Routes, args ...string) (map[string]any, int, *circlemock.Server) {
	t.Helper()
	cc, err := circlemock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cc.Close)
	endpoints := agentcli.DefaultEndpoints()
	endpoints.CircleCIAPIURL = cc.APIURL()
	stdout := &bytes.Buffer{}
	r := &runner{
		gate:   func(bool) error { return nil },
		stdout: stdout,
		requireCircleCI: func(context.Context) (authstore.Token, error) {
			return authstore.Token{Value: "token_test", Login: "someone"}, nil
		},
		endpoints: func() agentcli.Endpoints { return endpoints },
	}
	err = r.Run(&cobra.Command{}, args)
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("document: %v", err)
	}
	return doc, agentcli.Exit(err), cc
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
// and the tag's failed workflow is rerun from failed.
func TestRerunsTheTagPipeline(t *testing.T) {
	doc, code, _ := run(t, tagPipeline, "o/r", "1.2.3")
	if code != agentcli.ExitOK {
		t.Fatalf("exit %d: %v", code, doc["reason"])
	}
	workflows := doc["workflows"].([]any)
	if doc["tag"] != "v1.2.3" || doc["headSha"] != "abc" || len(workflows) != 1 || workflows[0].(map[string]any)["rerunId"] != "w2" {
		t.Errorf("document: %v", doc)
	}
}

// TestUnknownTag: a tag among no recent pipeline is exit 3, nothing posted.
func TestUnknownTag(t *testing.T) {
	doc, code, cc := run(t, tagPipeline, "o/r", "v9.9.9")
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
	if doc, code, _ := run(t, sequence.Routes{}, "o/r"); code != agentcli.ExitUsage || doc["command"] != "release rerun" {
		t.Fatalf("exit %d: %v", code, doc)
	}
}
