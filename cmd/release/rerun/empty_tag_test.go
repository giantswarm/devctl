package rerun

import (
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// An unset shell variable passed as "$TAG" is an empty argument, not a
// request to recover the newest branch pipeline (whose tag is also empty).
func TestEmptyTagDoesNotRecoverBranchPipeline(t *testing.T) {
	for _, state := range []string{"failed", "wedged"} {
		t.Run(state, func(t *testing.T) {
			cc := sequence.Routes{}
			for route, responses := range tagPipeline {
				cc[route] = responses
			}
			if state == "wedged" {
				cc = runningTagPipeline(3*time.Hour, "canceled")
			}
			cc["GET /api/v2/project/gh/o/r/pipeline"] = []sequence.Response{{Body: map[string]any{"items": []any{
				map[string]any{"id": "p1", "number": 12, "vcs": map[string]any{"branch": "main", "revision": "abc"}},
			}}}}
			doc, code, gh, circleci := run(t, sequence.Routes{}, cc, "o/r", "")
			if code != agentcli.ExitUsage || !strings.Contains(doc["reason"].(string), "the tag is empty") {
				t.Errorf("empty tag: exit %d, reason %v; CircleCI writes %v", code, doc["reason"], circleCIPosts(circleci))
			}
			if doc["command"] != command || doc["verdict"] != string(agentcli.VerdictUsage) {
				t.Errorf("usage envelope: %v", doc)
			}
			if len(circleci.Requests()) != 0 || len(gh.Requests()) != 0 {
				t.Errorf("empty tag contacted services: CircleCI %v, GitHub %v", circleci.Requests(), gh.Requests())
			}
		})
	}
}
