package rerun

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// A stable tag, its candidate and main can share a commit. Recovery must
// send the stable tag's push again, even when newer deliveries match its SHA.
func TestStalledTagRecoveryBindsTheResolvedRef(t *testing.T) {
	for _, targetDelivery := range []bool{true, false} {
		t.Run(fmt.Sprintf("target_delivery_%t", targetDelivery), func(t *testing.T) {
			created := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			cc := sequence.Routes{
				"GET /api/v2/project/gh/giantswarm/r/pipeline": {{Body: map[string]any{"items": []any{
					map[string]any{"id": "p1", "number": 12, "state": "pending", "created_at": created,
						"vcs": map[string]any{"tag": "v1.2.3", "revision": "abc"}},
				}}}},
				"GET /api/v2/pipeline/p1/workflow": {{Body: map[string]any{"items": []any{}}}},
				"POST /api/v2/project/gh/giantswarm/r/pipeline": {{Status: http.StatusCreated,
					Body: map[string]any{"id": "p2", "number": 13, "state": "pending"}}},
			}
			gh := sequence.Routes{
				"GET /repos/giantswarm/r/hooks": {{Body: []any{
					map[string]any{"id": 1, "active": true, "config": map[string]any{"url": "https://circleci.com/hooks/github"}},
				}}},
			}
			refs := []string{"refs/heads/main", "refs/tags/v1.2.3-rc.1"}
			if targetDelivery {
				refs = append(refs, "refs/tags/v1.2.3")
			}
			var deliveries []any
			for i, ref := range refs {
				id := 44 - i
				guid := fmt.Sprintf("push-%d", id)
				deliveries = append(deliveries, map[string]any{
					"id": id, "guid": guid, "event": "push", "redelivery": false,
					"delivered_at": time.Now().Add(-time.Duration(i+1) * time.Minute).UTC().Format(time.RFC3339),
				})
				path := fmt.Sprintf("/repos/giantswarm/r/hooks/1/deliveries/%d", id)
				gh["GET "+path] = []sequence.Response{{Body: map[string]any{
					"id": id, "guid": guid, "event": "push", "redelivery": false,
					"request": map[string]any{"payload": map[string]any{"ref": ref, "after": "abc"}},
				}}}
				// Every POST is accepted so selecting the wrong ref fails our
				// target assertions, rather than an unrelated mock 404.
				gh["POST "+path+"/attempts"] = []sequence.Response{{Status: http.StatusAccepted}}
			}
			gh["GET /repos/giantswarm/r/hooks/1/deliveries"] = []sequence.Response{{Body: deliveries}}
			// The caller's bare version resolves to the v-prefixed tag. The
			// webhook ref must use that resolved spelling too.
			doc, code, github, circleci := run(t, gh, cc, "giantswarm/r", "1.2.3")
			if code != agentcli.ExitOK || doc["tag"] != "v1.2.3" || doc["headSha"] != "abc" {
				t.Fatalf("exit %d: %v", code, doc)
			}
			var posts []string
			for _, request := range github.Requests() {
				if request.Method == http.MethodPost {
					posts = append(posts, request.Path)
				}
			}
			if targetDelivery {
				if len(posts) != 1 || posts[0] != "/repos/giantswarm/r/hooks/1/deliveries/42/attempts" {
					t.Errorf("GitHub writes: %v", posts)
				}
				redelivery, _ := doc["redelivery"].(map[string]any)
				if redelivery["ref"] != "refs/tags/v1.2.3" || redelivery["deliveryId"] != float64(42) || redelivery["outcome"] != "redelivered" {
					t.Errorf("redelivery: %v", redelivery)
				}
				if writes := circleCIPosts(circleci); len(writes) != 0 {
					t.Errorf("unexpected CircleCI writes: %v", writes)
				}
			} else {
				if len(posts) != 0 {
					t.Errorf("redelivered a different ref: %v", posts)
				}
				if writes := circleCIPosts(circleci); len(writes) != 1 || writes[0] != "/api/v2/project/gh/giantswarm/r/pipeline" {
					t.Errorf("fresh tag pipeline writes: %v", writes)
				}
				trigger, _ := doc["trigger"].(map[string]any)
				pipeline, _ := trigger["pipeline"].(map[string]any)
				if trigger["outcome"] != "triggered" || pipeline["id"] != "p2" {
					t.Errorf("trigger: %v", trigger)
				}
			}
		})
	}
}
