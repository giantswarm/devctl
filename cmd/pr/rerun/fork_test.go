package rerun

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// A fork can point at a commit also pushed to an upstream branch (for
// example a backport PR). That upstream push is not the fork's push and
// redelivering it cannot recover the pull/<number> pipeline.
func TestStalledForkDoesNotReadBaseHooks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hooks []sequence.Response
	}{
		{
			name: "base push has the same revision",
			hooks: []sequence.Response{{Body: []any{map[string]any{
				"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"},
			}}}},
		},
		{
			name:  "base hooks are forbidden",
			hooks: []sequence.Response{{Status: http.StatusForbidden, Body: map[string]any{"message": "Resource not accessible by integration"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, gh, cc := start(t, sequence.Routes{
				"GET /repos/o/r/pulls/7": pull("someone/r"),
				"GET /repos/o/r/hooks":   tc.hooks,
				"GET /repos/o/r/hooks/1/deliveries": {{Body: []any{map[string]any{
					"id": 42, "guid": "g-base-push", "event": "push", "redelivery": false,
					"delivered_at": "2026-10-09T17:59:00Z",
				}}}},
				"GET /repos/o/r/hooks/1/deliveries/42": {{Body: map[string]any{
					"id": 42, "guid": "g-base-push", "event": "push", "redelivery": false,
					"delivered_at": "2026-10-09T17:59:00Z",
					"request":      map[string]any{"payload": map[string]any{"ref": "refs/heads/main", "after": "abc"}},
				}}},
				"POST /repos/o/r/hooks/1/deliveries/42/attempts": {{Status: http.StatusAccepted}},
			}, sequence.Routes{
				"GET /api/v2/project/gh/o/r/pipeline?branch=pull%2F7": {{Body: map[string]any{"items": []any{
					map[string]any{"id": "p1", "number": 12, "state": "pending",
						"created_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
						"vcs":        map[string]any{"revision": "abc"}},
				}}}},
				"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{}}}},
			})
			doc, code := execute(t, r, "o/r", "7")
			if code != agentcli.ExitNotApplicable || doc["verdict"] != string(agentcli.VerdictNotApplicable) ||
				!strings.Contains(doc["reason"].(string), "fork") {
				t.Errorf("want exit 3 explaining the fork, got exit %d: %v", code, doc["reason"])
			}
			if _, ok := doc["redelivery"]; ok {
				t.Errorf("fork must not select a base delivery: %v", doc["redelivery"])
			}
			if doc["headSha"] != "abc" || doc["pipeline"].(map[string]any)["id"] != "p1" {
				t.Errorf("lost fork pipeline evidence: %v", doc)
			}
			if requests := gh.Requests(); len(requests) != 1 || requests[0].Path != "/repos/o/r/pulls/7" {
				t.Errorf("base webhooks were accessed: %v", requests)
			}
			for _, req := range cc.Requests() {
				if req.Method != http.MethodGet {
					t.Errorf("unexpected CircleCI write: %v", req)
				}
			}
		})
	}
}
