package prwait

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func Test_Wait_noCircleCIPipelines(t *testing.T) {
	actions := func(status, conclusion string) sequence.Response {
		return body(map[string]any{"total_count": 1, "workflow_runs": []any{map[string]any{
			"id": 20, "name": "Tofu Plan", "status": status, "conclusion": conclusion,
		}}})
	}
	for _, tc := range []struct {
		name      string
		checks    []sequence.Response
		actions   []sequence.Response
		required  []any
		wantCode  int
		wantPolls int
	}{
		{name: "green", checks: []sequence.Response{checkRuns(run("test", "completed", "success"))}, wantPolls: 1},
		{name: "red", checks: []sequence.Response{checkRuns(run("test", "completed", "failure"))}, wantCode: agentcli.ExitRed, wantPolls: 1},
		{name: "pending then green", checks: []sequence.Response{checkRuns(run("test", "in_progress", "")), checkRuns(run("test", "in_progress", "")), checkRuns(run("test", "completed", "success"))}, wantPolls: 3},
		{name: "Actions pending then green", checks: []sequence.Response{checkRuns(run("test", "completed", "success"))}, actions: []sequence.Response{actions("in_progress", ""), actions("in_progress", ""), actions("completed", "success")}, wantPolls: 3},
		{name: "required CircleCI context missing", checks: []sequence.Response{checkRuns(run("test", "completed", "success"))}, required: []any{"ci/circleci: go-build"}, wantCode: agentcli.ExitRequiredMissing, wantPolls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			gh[runsPath] = tc.checks
			if tc.actions != nil {
				gh[actsPath] = tc.actions
			}
			if tc.required != nil {
				gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{body(map[string]any{
					"required_status_checks": map[string]any{"contexts": tc.required},
				})}
			}
			none := body(map[string]any{"items": []any{}, "next_page_token": nil})
			h := start(t, gh, sequence.Routes{projPath: {project}, pipePath: {none}, listPath: {none}}, true, 2*time.Minute)
			h.waiter.rate = nil
			result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if exitCode(err) != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, exitCode(err), err)
			}
			if result.CircleCI != nil {
				t.Errorf("want no CircleCI pipeline in the result, got %+v", result.CircleCI)
			}
			if len(result.Warnings) != 1 {
				t.Fatalf("want one absent-pipeline warning, got %q", result.Warnings)
			}
			for _, part := range []string{sha, "o/r", CircleCIConfigPath, "no listed CircleCI pipelines", "GitHub alone"} {
				if !strings.Contains(result.Warnings[0], part) {
					t.Errorf("warning %q must name %q", result.Warnings[0], part)
				}
			}
			polls := 0
			for _, request := range h.github.Requests() {
				if request.Path == "/repos/o/r/commits/abc123/check-runs" {
					polls++
				}
			}
			if polls != tc.wantPolls {
				t.Errorf("want %d check polls, got %d", tc.wantPolls, polls)
			}
			doc := NewDocument(time.Now())
			doc.Take(result)
			data, marshalErr := json.Marshal(doc)
			if marshalErr != nil || !strings.Contains(string(data), result.Warnings[0]) {
				t.Errorf("document must carry the absent-pipeline warning: %s, %v", data, marshalErr)
			}
		})
	}
}

func Test_Wait_absentPipelineKeepsCircleCIEvidence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		listing  sequence.Response
		statuses sequence.Response
		wantCode int
	}{
		{name: "base pipeline", listing: body(map[string]any{"items": []any{map[string]any{"id": "base", "vcs": map[string]any{"revision": "base123", "branch": "main"}}}}), wantCode: agentcli.ExitTimeout},
		{name: "filtered base but another branch built", listing: body(map[string]any{"items": []any{map[string]any{"id": "other", "vcs": map[string]any{"revision": "other123", "branch": "other"}}}}), wantCode: agentcli.ExitTimeout},
		{name: "empty page with a next page", listing: body(map[string]any{"items": []any{}, "next_page_token": "next"}), wantCode: agentcli.ExitTimeout},
		{name: "head has a CircleCI status", statuses: successStatuses("ci/circleci: setup"), wantCode: agentcli.ExitTimeout},
		{name: "listing forbidden", listing: sequence.Response{Status: http.StatusForbidden, Body: map[string]any{"message": "forbidden"}}, wantCode: agentcli.ExitUsage},
		{name: "listing not found", listing: sequence.Response{Status: http.StatusNotFound, Body: map[string]any{"message": "not found"}}, wantCode: agentcli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			if tc.statuses.Body != nil {
				gh[statPath] = []sequence.Response{tc.statuses}
			}
			none := body(map[string]any{"items": []any{}})
			h := start(t, gh, sequence.Routes{projPath: {project}, pipePath: {none}, listPath: {tc.listing}}, true, time.Minute)
			h.waiter.rate = nil
			_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if exitCode(err) != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, exitCode(err), err)
			}
			if strings.Contains(h.progress.String(), "GitHub alone") {
				t.Errorf("CircleCI evidence or an unreadable listing must not be skipped:\n%s", h.progress.String())
			}
		})
	}
}

func Test_Wait_pipelineAppearsWhileGitHubIsPending(t *testing.T) {
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	gh[runsPath] = []sequence.Response{checkRuns(run("test", "in_progress", "")), checkRuns(run("test", "completed", "success"))}
	none := body(map[string]any{"items": []any{}})
	cc := sequence.Routes{
		projPath: {project},
		pipePath: {none, pipelines},
		listPath: {none},
		wfPath:   {body(map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": "failed"}}})},
	}
	h := start(t, gh, cc, true, time.Minute)
	h.waiter.rate = nil
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRed || !strings.Contains(err.Error(), "circleci workflow build failed") {
		t.Fatalf("want the newly appeared pipeline's failure, got %d: %v", exitCode(err), err)
	}
	if result.CircleCI == nil || result.CircleCI.PipelineID != "p1" {
		t.Errorf("want the appeared pipeline, got %+v", result.CircleCI)
	}
}
