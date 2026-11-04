package wait

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

// A gh token refused during a wait must not be replaced with the devctl
// App's token, even when an App login is available on the same machine.
func Test_run_refusedGHLoginKeepsIdentity(t *testing.T) {
	checks := func(status, conclusion string) sequence.Response {
		return sequence.Response{Body: map[string]any{"total_count": 1, "check_runs": []any{
			map[string]any{"id": 1, "name": "go-build", "status": status, "conclusion": conclusion},
		}}}
	}
	routes := sequence.Routes{
		"GET /repos/o/r/pulls/42": {{Body: map[string]any{
			"number": 42, "state": "open", "mergeable_state": "clean",
			"head": map[string]any{"sha": "abc123", "ref": "feature"}, "base": map[string]any{"ref": "main"},
		}}},
		"GET /repos/o/r/commits/abc123/check-runs": {
			checks("in_progress", ""),
			{Status: http.StatusUnauthorized, Body: map[string]any{"message": "Bad credentials", "status": "401"}},
			// A credential switch could otherwise hide the refusal with green.
			checks("completed", "success"),
		},
		"GET /repos/o/r/commits/abc123/status":        {{Body: map[string]any{"state": "success", "total_count": 0, "statuses": []any{}}}},
		"GET /repos/o/r/actions/runs?head_sha=abc123": {{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}},
	}
	r, stdout, _, server := newRunner(t, routes, func(context.Context) (authstore.Token, error) {
		t.Fatal("a repository outside the App owners must not read the App login")
		return authstore.Token{}, nil
	})
	r.personGitHub = func(context.Context) (authstore.Token, error) {
		return authstore.Token{Value: "gho_person_fixture", Source: authexec.SourceGHLogin}, nil
	}
	renewals := 0
	r.renewGitHub = func(context.Context, string) (string, error) {
		renewals++
		return "ghu_app_fixture", nil
	}
	clock := agentcli.NewVirtualClock(time.Now())
	r.clock = func() (agentcli.Clock, error) { return clock, nil }

	err := r.run(context.Background(), []string{"o/r", "42"})
	if agentcli.Exit(err) != agentcli.ExitUsage {
		t.Errorf("want the refused read's exit 7, got %v", err)
	}
	if renewals != 0 {
		t.Errorf("a refused gh login asked to renew the App login %d time(s)", renewals)
	}
	doc := decode(t, stdout)
	if doc["identity"] != "gh" || doc["verdict"] != "usage" || doc["exitCode"] != 7.0 {
		t.Errorf("want the refusal with identity gh, got %v", doc)
	}
	if reason, _ := doc["reason"].(string); !strings.Contains(reason, "401 Bad credentials") {
		t.Errorf("the document lost GitHub's refusal: %v", doc)
	}
	if doc["repository"] != "o/r" || doc["number"] != 42.0 || doc["headSha"] != "abc123" {
		t.Errorf("the document lost the head being waited for: %v", doc)
	}
	previous, _ := doc["checks"].([]any)
	if len(previous) != 1 || previous[0].(map[string]any)["status"] != "in_progress" {
		t.Errorf("want the last complete poll's pending checks, got %v", previous)
	}
	checkReads := 0
	for _, req := range server.Requests() {
		if req.Header.Get("Authorization") != "Bearer gho_person_fixture" {
			t.Error("a request switched away from the person's gh login")
		}
		if req.Path == "/repos/o/r/commits/abc123/check-runs" {
			checkReads++
		}
	}
	if checkReads != 2 {
		t.Errorf("want the pending read and the refused read, got %d check reads", checkReads)
	}
}
