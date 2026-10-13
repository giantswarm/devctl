package ci

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// run is one workflow of pipeline 3885 of o/r as the workflow endpoints
// answer it.
func run(id, name, status, createdAt string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "status": status, "created_at": createdAt, "stopped_at": nil,
		"pipeline_id": "p-3885", "pipeline_number": 3885, "project_slug": "gh/o/r", "started_by": "someone",
	}
}

func rerunRoutes(runs ...map[string]any) sequence.Routes {
	items := make([]any, 0, len(runs))
	routes := sequence.Routes{}
	for _, r := range runs {
		items = append(items, r)
		routes["GET /api/v2/workflow/"+r["id"].(string)] = []sequence.Response{{Body: r}}
		routes["POST /api/v2/workflow/"+r["id"].(string)+"/rerun"] = []sequence.Response{{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w-new"}}}
	}
	routes["GET /api/v2/pipeline/p-3885/workflow"] = []sequence.Response{{Body: map[string]any{"items": items, "next_page_token": nil}}}
	return routes
}

func rerun(t *testing.T, client *circleciclient.Client, workflowID string, opts RerunOptions) (*RerunResult, []string, error) {
	t.Helper()
	result := NewRerunResult("o/r")
	var warnings []string
	err := Rerun(context.Background(), client, "o", "r", workflowID, opts, agentcli.NewVirtualClock(now), result, func(m string) { warnings = append(warnings, m) })
	return result, warnings, err
}

func posted(srv *circlemock.Server, path string) bool {
	for _, r := range srv.Requests() {
		if r.Method == http.MethodPost && r.Path == path {
			return true
		}
	}
	return false
}

// TestRerunsAFinishedWorkflow: the one run of a name that failed is rerun;
// the document carries the pipeline, the workflow and the new run.
func TestRerunsAFinishedWorkflow(t *testing.T) {
	client, srv := newClient(t, rerunRoutes(run("w1", "setup", "success", "2026-10-09T17:00:00Z"), run("w2", "build", "failed", "2026-10-09T17:01:00Z")))
	result, warnings, err := rerun(t, client, "w2", RerunOptions{})
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if !posted(srv, "/api/v2/workflow/w2/rerun") {
		t.Errorf("no rerun posted: %v", srv.Requests())
	}
	if result.Outcome != OutcomeRerun || result.RerunID != "w-new" || result.RerunURL != "https://app.circleci.com/pipelines/github/o/r/3885/workflows/w-new" || result.Canceled || result.FromFailed {
		t.Errorf("result: %+v", result)
	}
	if result.Pipeline == nil || result.Pipeline.Number != 3885 || result.Workflow == nil || result.Workflow.Name != "build" || result.LastRerun != nil || len(warnings) != 0 {
		t.Errorf("result: pipeline %+v workflow %+v lastRerun %+v warnings %v", result.Pipeline, result.Workflow, result.LastRerun, warnings)
	}
}

// TestRerunWithinTheHourIsRefused: the name was rerun 30 minutes ago;
// whichever of its runs is named, nothing is posted and the document names
// the rerun.
func TestRerunWithinTheHourIsRefused(t *testing.T) {
	for _, id := range []string{"w3", "w2"} {
		client, srv := newClient(t, rerunRoutes(run("w2", "build", "failed", "2026-10-09T16:00:00Z"), run("w3", "build", "failed", "2026-10-09T17:30:00Z")))
		result, _, err := rerun(t, client, id, RerunOptions{})
		if agentcli.Exit(err) != agentcli.ExitRefused {
			t.Fatalf("%s: %v, want exit 5", id, err)
		}
		if !strings.Contains(err.Error(), "rerun 30m0s ago as w3") || !strings.Contains(err.Error(), "after 2026-10-09T18:30:00Z") {
			t.Errorf("%s: reason %q", id, err)
		}
		if result.Outcome != OutcomeRefused || result.LastRerun == nil || result.LastRerun.ID != "w3" {
			t.Errorf("%s: result %+v lastRerun %+v", id, result, result.LastRerun)
		}
		if posted(srv, "/api/v2/workflow/w3/rerun") || posted(srv, "/api/v2/workflow/w2/rerun") {
			t.Errorf("%s: a rerun was posted: %v", id, srv.Requests())
		}
	}
}

// TestRerunAfterTheHourIsAllowed: the last rerun is older than an hour; an
// older run named instead of the newest is rerun as asked, with a warning.
func TestRerunAfterTheHourIsAllowed(t *testing.T) {
	client, srv := newClient(t, rerunRoutes(run("w2", "build", "failed", "2026-10-09T16:00:00Z"), run("w3", "build", "failed", "2026-10-09T16:55:00Z")))
	if result, warnings, err := rerun(t, client, "w3", RerunOptions{FromFailed: true}); err != nil || result.Outcome != OutcomeRerun || !result.FromFailed || len(warnings) != 0 {
		t.Errorf("w3: err=%v result=%+v warnings=%v", err, result, warnings)
	}
	if !posted(srv, "/api/v2/workflow/w3/rerun") {
		t.Errorf("no rerun of w3 posted: %v", srv.Requests())
	}

	client, srv = newClient(t, rerunRoutes(run("w2", "build", "failed", "2026-10-09T16:00:00Z"), run("w3", "build", "failed", "2026-10-09T16:55:00Z")))
	result, warnings, err := rerun(t, client, "w2", RerunOptions{})
	if err != nil || result.Outcome != OutcomeRerun || !posted(srv, "/api/v2/workflow/w2/rerun") {
		t.Fatalf("w2: err=%v result=%+v", err, result)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "w2 is not the newest run of build, w3 (failed) is") {
		t.Errorf("w2 warnings: %v", warnings)
	}
}

// TestRunningIsRefusedWithoutCancel: a workflow still running is not rerun
// and not canceled.
func TestRunningIsRefusedWithoutCancel(t *testing.T) {
	client, srv := newClient(t, rerunRoutes(run("w2", "build", "running", "2026-10-09T17:01:00Z")))
	result, _, err := rerun(t, client, "w2", RerunOptions{})
	if agentcli.Exit(err) != agentcli.ExitRefused || !strings.Contains(err.Error(), "still running") || !strings.Contains(err.Error(), "--cancel") {
		t.Fatalf("running: %v, want exit 5 naming --cancel", err)
	}
	if result.Outcome != OutcomeRefused || result.Canceled || posted(srv, "/api/v2/workflow/w2/cancel") || posted(srv, "/api/v2/workflow/w2/rerun") {
		t.Errorf("running: result=%+v requests=%v", result, srv.Requests())
	}
}

// TestCancelThenRerun: --cancel posts the cancel, waits until the workflow
// reads canceled and reruns it.
func TestCancelThenRerun(t *testing.T) {
	routes := rerunRoutes(run("w2", "build", "running", "2026-10-09T17:01:00Z"))
	canceled := run("w2", "build", "canceled", "2026-10-09T17:01:00Z")
	routes["GET /api/v2/workflow/w2"] = []sequence.Response{{Body: run("w2", "build", "running", "2026-10-09T17:01:00Z")}, {Body: run("w2", "build", "running", "2026-10-09T17:01:00Z")}, {Body: canceled}}
	routes["POST /api/v2/workflow/w2/cancel"] = []sequence.Response{{Status: http.StatusAccepted, Body: map[string]any{"message": "Accepted."}}}
	client, srv := newClient(t, routes)
	result, _, err := rerun(t, client, "w2", RerunOptions{Cancel: true})
	if err != nil {
		t.Fatalf("cancel then rerun: %v", err)
	}
	if !posted(srv, "/api/v2/workflow/w2/cancel") || !posted(srv, "/api/v2/workflow/w2/rerun") {
		t.Errorf("requests: %v", srv.Requests())
	}
	if !result.Canceled || result.Outcome != OutcomeRerun || result.Workflow.Status != "canceled" || result.RerunID != "w-new" {
		t.Errorf("result: %+v workflow %+v", result, result.Workflow)
	}
	cancelIndex, rerunIndex := -1, -1
	for i, r := range srv.Requests() {
		switch r.Method + " " + r.Path {
		case "POST /api/v2/workflow/w2/cancel":
			cancelIndex = i
		case "POST /api/v2/workflow/w2/rerun":
			rerunIndex = i
		}
	}
	if cancelIndex > rerunIndex {
		t.Errorf("the rerun was posted before the cancel: %v", srv.Requests())
	}
}

// TestCancelThatNeverSettlesIsATimeout: a workflow that still reads running
// two minutes after the cancel is exit 2, and nothing is rerun.
func TestCancelThatNeverSettlesIsATimeout(t *testing.T) {
	routes := rerunRoutes(run("w2", "build", "running", "2026-10-09T17:01:00Z"))
	routes["POST /api/v2/workflow/w2/cancel"] = []sequence.Response{{Status: http.StatusAccepted, Body: map[string]any{"message": "Accepted."}}}
	client, srv := newClient(t, routes)
	result, _, err := rerun(t, client, "w2", RerunOptions{Cancel: true})
	if agentcli.Exit(err) != agentcli.ExitTimeout || !strings.Contains(err.Error(), "still reads running after 2m0s") {
		t.Fatalf("never settles: %v, want exit 2", err)
	}
	if !result.Canceled || posted(srv, "/api/v2/workflow/w2/rerun") {
		t.Errorf("result=%+v requests=%v", result, srv.Requests())
	}
}

// TestOtherRepositoryIsNotApplicable: a workflow of another project is exit
// 3 before anything else is read.
func TestOtherRepositoryIsNotApplicable(t *testing.T) {
	other := run("w2", "build", "failed", "2026-10-09T17:01:00Z")
	other["project_slug"] = "gh/o/other"
	client, srv := newClient(t, sequence.Routes{"GET /api/v2/workflow/w2": {{Body: other}}})
	result, _, err := rerun(t, client, "w2", RerunOptions{})
	if agentcli.Exit(err) != agentcli.ExitNotApplicable || !strings.Contains(err.Error(), "belongs to gh/o/other") {
		t.Fatalf("other project: %v, want exit 3", err)
	}
	if result.Pipeline != nil || len(srv.Requests()) != 1 {
		t.Errorf("result=%+v requests=%v", result, srv.Requests())
	}
}

// TestUnknownWorkflowIsNotApplicable: CircleCI answers 404: exit 3.
func TestUnknownWorkflowIsNotApplicable(t *testing.T) {
	client, _ := newClient(t, sequence.Routes{})
	if _, _, err := rerun(t, client, "w-none", RerunOptions{}); agentcli.Exit(err) != agentcli.ExitNotApplicable {
		t.Errorf("unknown: %v, want exit 3", err)
	}
}

// TestRerunCircleCIRefuses: a rerun CircleCI answers 400 to is exit 5 with
// its message; one it answers 403 to is exit 8 naming the Write login.
func TestRerunCircleCIRefuses(t *testing.T) {
	routes := rerunRoutes(run("w2", "build", "success", "2026-10-09T17:01:00Z"))
	routes["POST /api/v2/workflow/w2/rerun"] = []sequence.Response{{Status: http.StatusBadRequest, Body: map[string]any{"message": "Workflow has no failed jobs"}}}
	client, _ := newClient(t, routes)
	result, _, err := rerun(t, client, "w2", RerunOptions{FromFailed: true})
	if agentcli.Exit(err) != agentcli.ExitRefused || !strings.Contains(err.Error(), "Workflow has no failed jobs") || result.Outcome != OutcomeRefused {
		t.Errorf("400: err=%v result=%+v, want exit 5 with CircleCI's message", err, result)
	}

	routes["POST /api/v2/workflow/w2/rerun"] = []sequence.Response{{Status: http.StatusForbidden, Body: map[string]any{"message": "Permission denied"}}}
	client, _ = newClient(t, routes)
	if _, _, err := rerun(t, client, "w2", RerunOptions{}); agentcli.Exit(err) != agentcli.ExitAuthRequired || !strings.Contains(err.Error(), "Write") {
		t.Errorf("403: %v, want exit 8 naming Write access", err)
	}
}

// TestRunsOfName: the newest run of a name and how many there are.
func TestRunsOfName(t *testing.T) {
	runs := []circleciclient.Workflow{
		{ID: "w2", Name: "build", CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "w1", Name: "setup", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "w3", Name: "build", CreatedAt: now.Add(-time.Hour)},
	}
	if newest, count := runsOfName(runs, "build"); newest == nil || newest.ID != "w3" || count != 2 {
		t.Errorf("build: newest=%+v count=%d", newest, count)
	}
	if newest, count := runsOfName(runs, "setup"); newest == nil || newest.ID != "w1" || count != 1 {
		t.Errorf("setup: newest=%+v count=%d", newest, count)
	}
	if newest, count := runsOfName(runs, "test"); newest != nil || count != 0 {
		t.Errorf("test: newest=%+v count=%d", newest, count)
	}
}
