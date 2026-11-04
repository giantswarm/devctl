package prwait

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func Test_Wait_haltedSetup(t *testing.T) {
	// The job name differs from the workflow name, as on releases. Both
	// setup workflows succeed, but step halt skips continuation/continue.
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	gh[runsPath] = []sequence.Response{checkRuns(run("lint", "completed", "success"), gatekeeper())}
	gh[statPath] = []sequence.Response{successStatuses("ci/circleci: setup-retag", "ci/circleci: validate")}
	stopped := time.Date(2026, time.October, 12, 7, 20, 0, 0, time.UTC)
	workflow := func(id, name string) map[string]any {
		return map[string]any{"id": id, "name": name, "status": "success", "tag": "setup", "stopped_at": stopped}
	}
	cc := sequence.Routes{
		projPath: {project},
		listPath: {pipelines},
		pipePath: {body(map[string]any{"items": []any{map[string]any{
			"id": "p1", "number": 12, "state": "setup", "vcs": map[string]any{"revision": sha, "branch": "feature"},
		}}})},
		wfPath:                        {body(map[string]any{"items": []any{workflow("w1", "pr-retag-setup"), workflow("w2", "validate")}})},
		"GET /api/v2/workflow/w1/job": {body(map[string]any{"items": []any{map[string]any{"name": "setup-retag", "status": "success"}}})},
		"GET /api/v2/workflow/w2/job": {body(map[string]any{"items": []any{map[string]any{"name": "validate", "status": "success"}}})},
	}
	h := start(t, gh, cc, true, 6*time.Minute)
	// Start at completion: the grace measures time after the setup finished,
	// and does not restart when a new waiter observes an older halted setup.
	h.waiter.clock = agentcli.NewVirtualClock(stopped)
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green after halted setup, got %d %v", exitCode(err), err)
	}
	if elapsed := h.waiter.clock.Now().Sub(stopped); elapsed < 5*time.Minute || elapsed > 5*time.Minute+MaxInterval {
		t.Errorf("want five-minute continuation grace, got %s", elapsed)
	}
	if len(result.Blocking) != 1 || !strings.Contains(result.Blocking[0], "Heimdall") {
		t.Errorf("want the non-required gatekeeper named, got %q", result.Blocking)
	}
	if !strings.Contains(h.progress.String(), "setup halted, no continuation") {
		t.Errorf("want halted setup distinguished from listing lag:\n%s", h.progress.String())
	}
	if result.CircleCI == nil || !result.CircleCI.SetupHalted || len(result.CircleCI.Workflows) != 2 {
		t.Errorf("want both setup workflows and halted diagnosis, got %+v", result.CircleCI)
	}
}

func Test_evaluate_haltedSetup(t *testing.T) {
	stopped := time.Date(2026, time.October, 12, 7, 20, 0, 0, time.UTC)
	testCases := []struct {
		name        string
		change      func(*snapshot)
		wantGreen   bool
		wantRed     bool
		wantMissing bool
	}{
		{name: "case 0: setup halts at the grace boundary", wantGreen: true},
		{name: "case 1: continuation still has time to appear", change: func(s *snapshot) { s.now = s.now.Add(-time.Nanosecond) }},
		{name: "case 2: a submitted continuation stays pending", change: func(s *snapshot) { s.circleci.pipeline.State = "pending" }},
		{name: "case 3: a created pipeline with a lagging listing stays pending", change: func(s *snapshot) { s.circleci.pipeline.State = "created" }},
		{name: "case 4: queued setup stays pending", change: func(s *snapshot) { s.circleci.pipeline.State = "setup-pending" }},
		{name: "case 5: an empty listing stays pending", change: func(s *snapshot) { s.circleci.workflows = nil }},
		{name: "case 6: a workflow without a setup tag stays pending", change: func(s *snapshot) { s.circleci.workflows[0].Tag = "" }},
		{name: "case 7: missing completion time stays pending", change: func(s *snapshot) { s.circleci.workflows[0].StoppedAt = time.Time{} }},
		{name: "case 8: running setup stays pending", change: func(s *snapshot) { s.circleci.workflows[0].Status = "running" }},
		{name: "case 9: failed setup stays red", change: func(s *snapshot) { s.circleci.workflows[0].Status = "failed" }, wantRed: true},
		{name: "case 10: missing job listing stays pending", change: func(s *snapshot) { s.circleci.setupJobs = nil }},
		{name: "case 11: a workflow-named status cannot replace the job's context", change: func(s *snapshot) { s.statuses[0].Context = github.Ptr[string]("ci/circleci: pr-retag-setup") }},
		{name: "case 12: the latest job context is still pending", change: func(s *snapshot) {
			s.statuses[0].UpdatedAt = &github.Timestamp{Time: stopped}
			s.statuses = append(s.statuses, &github.RepoStatus{Context: github.Ptr[string]("ci/circleci: setup-retag"), State: github.Ptr[string]("pending"), UpdatedAt: &github.Timestamp{Time: stopped.Add(time.Second)}})
		}},
		{name: "case 13: the job listing still runs", change: func(s *snapshot) { s.circleci.setupJobs["w1"][0].Status = "running" }},
		{name: "case 14: another CircleCI context is evidence of continuation", change: func(s *snapshot) { s.statuses = append(s.statuses, status("ci/circleci: build", "success")) }},
		{name: "case 15: a failed head check stays red", change: func(s *snapshot) {
			s.checkRuns = []*github.CheckRun{checkRun(1, "lint", "completed", "failure", stopped)}
		}, wantRed: true},
		{name: "case 16: absent required context is never reported after setup halts", change: func(s *snapshot) { s.required = []string{"ci/circleci: build"} }, wantMissing: true},
		{name: "case 17: a newer setup rerun must finish too", change: func(s *snapshot) {
			s.circleci.workflows = append(s.circleci.workflows, circleciclient.Workflow{ID: "w2", Name: "pr-retag-setup", Tag: "setup", Status: "running", CreatedAt: stopped.Add(time.Second)})
		}},
		{name: "case 18: the grace starts after the last setup workflow finishes", change: func(s *snapshot) {
			s.circleci.workflows = append(s.circleci.workflows, circleciclient.Workflow{ID: "w2", Name: "validate", Tag: "setup", Status: "success", StoppedAt: stopped.Add(time.Second)})
			s.circleci.setupJobs["w2"] = []circleciclient.Job{{Name: "validate", Status: "success"}}
			s.statuses = append(s.statuses, status("ci/circleci: validate", "success"))
		}},
		{name: "case 19: a failed earlier setup run no longer counts", change: func(s *snapshot) {
			s.circleci.workflows[0].CreatedAt = stopped.Add(-time.Minute)
			s.circleci.workflows = append(s.circleci.workflows, circleciclient.Workflow{ID: "old", Name: "pr-retag-setup", Tag: "setup", Status: "failed", CreatedAt: stopped.Add(-2 * time.Minute)})
		}, wantGreen: true},
		{name: "case 20: successful context cannot hide a failed setup job", change: func(s *snapshot) { s.circleci.setupJobs["w1"][0].Status = "failed" }},
		{name: "case 21: every setup job needs its own successful context", change: func(s *snapshot) {
			s.circleci.setupJobs["w1"] = append(s.circleci.setupJobs["w1"], circleciclient.Job{Name: "validate", Status: "success"})
		}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := snapshot{
				now:      stopped.Add(5 * time.Minute),
				statuses: []*github.RepoStatus{status("ci/circleci: setup-retag", "success")},
				circleci: &circleSnapshot{
					pipeline:  &circleciclient.Pipeline{ID: "p1", Number: 12, State: "setup"},
					workflows: []circleciclient.Workflow{{ID: "w1", Name: "pr-retag-setup", Tag: "setup", Status: "success", StoppedAt: stopped}},
					setupJobs: map[string][]circleciclient.Job{"w1": {{Name: "setup-retag", Status: "success"}}},
				},
			}
			if tc.change != nil {
				tc.change(&s)
			}
			e := evaluate(s)
			if e.green() != tc.wantGreen || e.redDecided() != tc.wantRed || e.neverReported() != tc.wantMissing {
				t.Errorf("want green %v, red %v, required missing %v; got red %q, unfinished %q", tc.wantGreen, tc.wantRed, tc.wantMissing, e.red, e.unfinished)
			}
		})
	}
}

func Test_Wait_setupJobsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response sequence.Response
	}{
		{name: "case 0: job listing is empty", response: body(map[string]any{"items": []any{}})},
		{name: "case 1: workflow jobs are not found", response: sequence.Response{Status: 404, Body: map[string]any{"message": "Not Found"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			gh[statPath] = []sequence.Response{successStatuses("ci/circleci: setup-retag")}
			cc := sequence.Routes{
				projPath: {project}, listPath: {pipelines},
				pipePath: {body(map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12, "state": "setup", "vcs": map[string]any{"revision": sha, "branch": "feature"}}}})},
				wfPath: {body(map[string]any{"items": []any{map[string]any{
					"id": "w1", "name": "pr-retag-setup", "tag": "setup", "status": "success", "stopped_at": "2026-10-12T07:20:00Z",
				}}})},
				"GET /api/v2/workflow/w1/job": {tc.response},
			}
			h := start(t, gh, cc, true, time.Minute)
			h.waiter.clock = agentcli.NewVirtualClock(time.Date(2026, time.October, 12, 7, 25, 0, 0, time.UTC))
			_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if exitCode(err) != agentcli.ExitTimeout || strings.Contains(h.progress.String(), "setup halted") {
				t.Errorf("want incomplete job evidence to keep waiting, got %v, progress:\n%s", err, h.progress.String())
			}
			readJobs := false
			for _, request := range h.circleci.Requests() {
				readJobs = readJobs || request.Path == "/api/v2/workflow/w1/job"
			}
			if !readJobs {
				t.Error("want the incomplete workflow jobs read after the grace")
			}
		})
	}
}

func Test_Wait_lateSetupContinuation(t *testing.T) {
	stopped := time.Date(2026, time.October, 12, 7, 20, 0, 0, time.UTC)
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	gh[statPath] = []sequence.Response{successStatuses("ci/circleci: setup-retag"), successStatuses("ci/circleci: setup-retag", "ci/circleci: build")}
	pipeline := func(state string) sequence.Response {
		return body(map[string]any{"items": []any{map[string]any{"id": "p1", "number": 12, "state": state, "vcs": map[string]any{"revision": sha, "branch": "feature"}}}})
	}
	setup := setupWorkflow("success")
	setup["stopped_at"] = stopped
	cc := sequence.Routes{
		projPath: {project}, listPath: {pipelines},
		pipePath: {pipeline("setup"), pipeline("pending"), pipeline("created")},
		wfPath: {
			body(map[string]any{"items": []any{setup}}),
			body(map[string]any{"items": []any{setup}}),
			body(map[string]any{"items": []any{setup, map[string]any{"id": "w2", "name": "build", "status": "running"}}}),
			body(map[string]any{"items": []any{setup, map[string]any{"id": "w2", "name": "build", "status": "success"}}}),
		},
	}
	h := start(t, gh, cc, true, 2*time.Minute)
	h.waiter.clock = agentcli.NewVirtualClock(stopped.Add(5*time.Minute - MinInterval))
	// The mock's rate-reset headers use wall time, not this virtual date.
	h.waiter.rate = nil
	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil || result.CircleCI == nil || result.CircleCI.SetupHalted || len(result.CircleCI.Workflows) != 2 {
		t.Fatalf("want the continued build's success, got %+v, %v", result.CircleCI, err)
	}
	if !strings.Contains(h.progress.String(), "circleci workflow build (running)") || strings.Contains(h.progress.String(), "setup halted") {
		t.Errorf("want to wait for the late continuation, got:\n%s", h.progress.String())
	}
	for _, request := range h.circleci.Requests() {
		if strings.HasSuffix(request.Path, "/job") {
			t.Errorf("no halted-setup job read is due before the grace or after continuation: %s", request.String())
		}
	}
}
