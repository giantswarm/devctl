package prwait

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func Test_Wait_missingContinuationReason(t *testing.T) {
	testCases := []struct {
		name      string
		state     string
		workflows []any
		statuses  sequence.Response
		wantCause bool
	}{
		{name: "case 0: setup succeeded", state: "setup", workflows: []any{setupWorkflow("success")}, wantCause: true},
		{name: "case 1: continuation submitted", state: "pending", workflows: []any{setupWorkflow("success")}, wantCause: true},
		{name: "case 2: created without continuation", state: "created", workflows: []any{setupWorkflow("success")}, wantCause: true},
		{name: "case 3: setup still running", state: "setup", workflows: []any{setupWorkflow("running")}},
		{name: "case 4: setup failed", state: "setup", workflows: []any{setupWorkflow("failed")}},
		{name: "case 5: workflows unavailable", state: "pending"},
		{name: "case 6: continuation running", state: "created", workflows: []any{setupWorkflow("success"), map[string]any{"id": "w2", "name": "build", "status": "running"}}},
		{name: "case 7: continued jobs have posted", state: "pending", workflows: []any{setupWorkflow("success")}, statuses: successStatuses("ci/circleci: setup", "ci/circleci: go-test")},
		{name: "case 8: newer setup rerun still running", state: "setup", workflows: []any{
			map[string]any{"id": "w1", "name": "setup", "tag": "setup", "status": "success", "created_at": "2026-10-01T10:00:00Z"},
			map[string]any{"id": "w2", "name": "setup", "tag": "setup", "status": "running", "created_at": "2026-10-01T10:01:00Z"},
		}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			// A missing required job must keep waiting alongside the setup,
			// rather than ending as required_missing or red.
			gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{body(map[string]any{
				"required_status_checks": map[string]any{"contexts": []string{"ci/circleci: go-build"}},
			})}
			gh[statPath] = []sequence.Response{successStatuses("ci/circleci: setup")}
			if tc.statuses.Body != nil {
				gh[statPath] = []sequence.Response{tc.statuses}
			}
			cc := sequence.Routes{
				projPath: {project},
				pipePath: {body(map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12, "state": tc.state, "vcs": map[string]any{"revision": sha, "branch": "feature"}}}})},
				wfPath:   {body(map[string]any{"items": tc.workflows})},
			}
			h := start(t, gh, cc, true, 2*time.Minute)
			h.waiter.rate = nil
			started := h.waiter.clock.Now()
			result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if exitCode(err) != agentcli.ExitTimeout {
				t.Fatalf("want exit 2, got %d: %v", exitCode(err), err)
			}
			if got := strings.Contains(err.Error(), "CircleCI continuation missing: pipeline 12 exists and its setup succeeded, but no continuation workflow appeared before the deadline"); got != tc.wantCause {
				t.Errorf("missing continuation cause = %v, want %v: %v", got, tc.wantCause, err)
			}
			if elapsed := h.waiter.clock.Now().Sub(started); elapsed != 2*time.Minute {
				t.Errorf("wait ended after %s, want the configured 2m bound", elapsed)
			}
			if result.CircleCI == nil || result.CircleCI.PipelineID != "p1" || len(result.Unfinished) == 0 {
				t.Errorf("missing pipeline or unfinished evidence: %+v", result)
			}
		})
	}
}

func Test_Wait_failedLogPublicContinuation(t *testing.T) {
	blob := blobs(t, map[string]string{"/failed-step": `[{"message":"\u001b[31mcontinuation test failed\u001b[0m\r\n"}]`})
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	missing := sequence.Response{Status: http.StatusNotFound, Body: map[string]any{"message": "Workflow not found"}}
	cc := sequence.Routes{
		projPath: {project},
		pipePath: {pipelines},
		wfPath: {body(map[string]any{"items": []any{setupWorkflow("success"), map[string]any{
			"id": "continued", "name": "build", "status": "failed",
		}}})},
		"GET /api/v2/workflow/continued/job": {missing, body(map[string]any{"items": []any{
			map[string]any{"name": "go-test", "status": "failed", "job_number": 99},
			map[string]any{"name": "go-lint", "status": "success", "job_number": 98},
		}})},
		"GET /api/v1.1/project/github/o/r/99": {missing, body(map[string]any{"steps": []any{map[string]any{
			"actions": []any{map[string]any{"failed": true, "output_url": blob.URL + "/failed-step"}},
		}}})},
	}
	h := start(t, gh, cc, true, 2*time.Minute)
	h.waiter.failedLogLines = 2
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRed {
		t.Fatalf("want red, got %d: %v", exitCode(err), err)
	}
	if len(result.FailedJobs) != 1 || result.FailedJobs[0].Name != "build/go-test" || result.FailedJobs[0].LogTail != "continuation test failed" || result.FailedJobs[0].LogError != "" {
		t.Fatalf("want the continued job's clean log, got %+v", result.FailedJobs)
	}
	var printed strings.Builder
	PrintFailedJobs(&printed, result.FailedJobs)
	if !strings.Contains(printed.String(), "):\ncontinuation test failed\n") {
		t.Errorf("missing printed tail: %s", printed.String())
	}
	document, err := json.Marshal(result)
	if err != nil || !strings.Contains(string(document), `"logTail":"continuation test failed"`) || strings.Contains(string(document), `"logError"`) {
		t.Errorf("missing serialized tail: %s, error %v", document, err)
	}
	for _, path := range []string{"/api/v2/workflow/continued/job", "/api/v1.1/project/github/o/r/99"} {
		var headers []string
		for _, req := range h.circleci.Requests() {
			if req.Path == path {
				headers = append(headers, req.Header.Get("Circle-Token"))
			}
		}
		if len(headers) != 2 || headers[0] != "cci_test" || headers[1] != "" {
			t.Errorf("%s: want authenticated read then anonymous read, got %q", path, headers)
		}
	}
}

func TestContinuationTimeoutNeedsCurrentEvidence(t *testing.T) {
	testCases := []struct {
		name       string
		head       string
		unanswered string
		wantCause  bool
	}{
		{name: "case 0: current completed poll", head: sha, wantCause: true},
		{name: "case 1: last poll went unanswered", head: sha, unanswered: "CircleCI unavailable"},
		{name: "case 2: changed head partly read", head: "new-head"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			last := evaluate(snapshot{
				headSHA: sha,
				circleci: circle("pending", circleciclient.Workflow{
					ID: "setup", Name: "setup", Tag: circleciclient.WorkflowTagSetup, Status: "success",
				}),
			})
			waiter := &Waiter{timeout: time.Minute}
			result := &Result{HeadSHA: tc.head}
			err := waiter.timedOut(result, last, tc.unanswered)
			if exitCode(err) != agentcli.ExitTimeout || strings.Contains(err.Error(), "CircleCI continuation missing") != tc.wantCause {
				t.Errorf("want timeout with continuation cause %v, got %v", tc.wantCause, err)
			}
			if tc.unanswered != "" && !strings.Contains(err.Error(), tc.unanswered) {
				t.Errorf("unanswered poll was lost: %v", err)
			}
		})
	}
}

// deadlinePollGitHub interrupts the second poll after its checks were read,
// leaving the first poll's successful setup as the last complete evidence.
type deadlinePollGitHub struct {
	GitHub
	clock agentcli.Clock
	calls int
}

func (g *deadlinePollGitHub) WorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]*github.WorkflowRun, error) {
	g.calls++
	if g.calls == 2 {
		return nil, g.clock.Sleep(ctx, 2*time.Minute)
	}
	return g.GitHub.WorkflowRunsForSHA(ctx, owner, repo, sha)
}

func Test_Wait_deadlineDuringPollDoesNotDiagnoseContinuation(t *testing.T) {
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	gh[statPath] = []sequence.Response{successStatuses("ci/circleci: setup")}
	cc := sequence.Routes{
		projPath: {project},
		pipePath: {pipelines},
		wfPath:   {body(map[string]any{"items": []any{setupWorkflow("success")}})},
	}
	h := start(t, gh, cc, true, 2*time.Minute)
	h.waiter.rate = nil
	reader := &deadlinePollGitHub{GitHub: h.waiter.github, clock: h.waiter.clock}
	h.waiter.github = reader
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if reader.calls != 2 || exitCode(err) != agentcli.ExitTimeout {
		t.Fatalf("want second poll interrupted with exit 2, got %d calls: %v", reader.calls, err)
	}
	if strings.Contains(err.Error(), "CircleCI continuation missing") || !strings.Contains(err.Error(), "last poll went unanswered") {
		t.Errorf("want unknown head rather than stale continuation diagnosis: %v", err)
	}
	if result.CircleCI == nil || result.CircleCI.PipelineID != "p1" || len(result.Unfinished) == 0 {
		t.Errorf("last complete pipeline and unfinished evidence lost: %+v", result)
	}
}

// A deadline interrupting an ordinary queued-check poll retains the existing
// timeout reason. Only a previous continuation diagnosis needs invalidation.
func Test_Wait_deadlineDuringOrdinaryPollPreservesReason(t *testing.T) {
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns(run("go-lint", "queued", ""))}
	h := start(t, gh, nil, false, 2*time.Minute)
	h.waiter.rate = nil
	reader := &deadlinePollGitHub{GitHub: h.waiter.github, clock: h.waiter.clock}
	h.waiter.github = reader
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if reader.calls != 2 || exitCode(err) != agentcli.ExitTimeout {
		t.Fatalf("want second poll interrupted with exit 2, got %d calls: %v", reader.calls, err)
	}
	if err.Error() != "timeout after 2m0s; unfinished: check go-lint (queued)" || len(result.Unfinished) != 1 || result.Unfinished[0] != "check go-lint (queued)" {
		t.Errorf("ordinary timeout reason changed or unfinished evidence lost: %v, %+v", err, result.Unfinished)
	}
}
