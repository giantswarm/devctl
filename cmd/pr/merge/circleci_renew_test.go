package merge

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
)

func TestRunCircleCILoginRenewal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		persistent bool
	}{
		{name: "renewed login", status: 401},
		{name: "transient 5xx", status: 503},
		{name: "persistent 401", status: 401, persistent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := green()
			routes["GET /repos/o/r/contents/.circleci/config.yml"] = []sequence.Response{{Body: map[string]any{"type": "file", "path": ".circleci/config.yml", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("version: 2.1\n"))}}}
			r, stdout, _, github := newRunner(t, routes, loggedIn, &flag{Timeout: 2 * time.Minute, NoReleaseWait: true})
			workflow := func(status string) sequence.Response {
				return sequence.Response{Body: map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": status, "pipeline_number": 12}}}}
			}
			replies := []sequence.Response{workflow("running"), {Status: tc.status, Body: map[string]any{"message": "Invalid token provided"}}}
			if !tc.persistent {
				replies = append(replies, workflow("success"))
			}
			circle, err := circlemock.Start(sequence.Routes{
				"GET /api/v2/project/gh/o/r":          {{Body: map[string]any{"slug": "gh/o/r"}}},
				"GET /api/v2/project/gh/o/r/pipeline": {{Body: map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12, "vcs": map[string]any{"revision": "abc123"}}}}}},
				"GET /api/v2/pipeline/p1/workflow":    replies,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer circle.Close()
			endpoints := r.endpoints()
			endpoints.CircleCIAPIURL = circle.APIURL()
			r.endpoints = func() agentcli.Endpoints { return endpoints }
			r.clock = func() (agentcli.Clock, error) { return agentcli.NewVirtualClock(time.Now()), nil }
			reads := 0
			r.requireCircleCI = func(context.Context) (authstore.Token, error) {
				reads++
				value := "fixture-old"
				if reads > 1 {
					value = "fixture-new"
				}
				return authstore.Token{Value: value, Source: authstore.SourceKeychain}, nil
			}
			err = r.run(t.Context(), []string{"o/r", "42"})
			wantCode := 0
			if tc.persistent {
				wantCode = agentcli.ExitRemoteError
			}
			doc := decode(t, stdout)
			if agentcli.Exit(err) != wantCode || doc["exitCode"] != float64(wantCode) {
				t.Fatalf("exit=%d document=%v error=%v", agentcli.Exit(err), doc, err)
			}
			if tc.persistent && doc["verdict"] != "remote_error" {
				t.Fatalf("verdict=%v", doc["verdict"])
			}
			wantReads := 1
			if tc.status == 401 {
				wantReads = 2
			}
			if reads != wantReads {
				t.Fatalf("credential reads=%d, want %d", reads, wantReads)
			}
			workflowReads := 0
			for _, req := range circle.Requests() {
				if req.Path == "/api/v2/pipeline/p1/workflow" {
					workflowReads++
				}
				want := "fixture-old"
				if tc.status == 401 && workflowReads >= 3 {
					want = "fixture-new"
				}
				if req.Header.Get("Circle-Token") != want {
					t.Fatal("CircleCI request used the wrong credential")
				}
			}
			if workflowReads != 3 {
				t.Fatalf("workflow reads=%d, want pending, refused, retry", workflowReads)
			}
			merged := false
			for _, req := range github.Requests() {
				if req.Method == http.MethodPut && req.Path == "/repos/o/r/pulls/42/merge" {
					merged = true
				}
			}
			if merged == tc.persistent {
				t.Fatalf("mocked merge=%v after persistent refusal=%v", merged, tc.persistent)
			}
		})
	}
}
