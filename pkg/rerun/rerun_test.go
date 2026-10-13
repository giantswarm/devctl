package rerun

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/authstore"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

const (
	workflowsPath = "GET /api/v2/pipeline/p1/workflow"
)

func body(v any) []sequence.Response { return []sequence.Response{{Body: v}} }

func workflow(id, name, status, created string) map[string]any {
	return map[string]any{"id": id, "name": name, "status": status, "created_at": created}
}

func jobs(statuses ...string) []sequence.Response {
	items := []any{}
	for i, s := range statuses {
		items = append(items, map[string]any{"name": "job" + string(rune('a'+i)), "status": s, "type": "build"})
	}
	return body(map[string]any{"items": items})
}

func run(t *testing.T, routes sequence.Routes) (*Result, []string, *circlemock.Server, error) {
	t.Helper()
	server, err := circlemock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	client, err := circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result := NewResult("o/r")
	var warnings []string
	pipeline := &circleciclient.Pipeline{ID: "p1", Number: 12, VCS: circleciclient.PipelineVCS{Revision: "abc"}}
	err = FromFailed(context.Background(), client, "o", "r", pipeline, result, func(w string) { warnings = append(warnings, w) }, Redelivery{})
	return result, warnings, server, err
}

func posts(server *circlemock.Server) []string {
	var out []string
	for _, r := range server.Requests() {
		if r.Method == http.MethodPost {
			out = append(out, r.Path)
		}
	}
	return out
}

// TestRerunsTheNewestFailedRun: of a name rerun before only the newest run
// counts, a failed one is rerun from failed, a green one left alone.
func TestRerunsTheNewestFailedRun(t *testing.T) {
	result, warnings, server, err := run(t, sequence.Routes{
		workflowsPath: body(map[string]any{"items": []any{
			workflow("w0", "build", "failed", "2026-10-09T10:00:00Z"),
			workflow("w1", "build", "failed", "2026-10-09T11:00:00Z"),
			workflow("w2", "lint", "success", "2026-10-09T10:00:00Z"),
		}}),
		"GET /api/v2/workflow/w1/job":    jobs("success", "failed", "blocked"),
		"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w3"}}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := posts(server); len(got) != 1 || got[0] != "/api/v2/workflow/w1/rerun" {
		t.Errorf("reruns: %q, want w1 alone", got)
	}
	if len(result.Workflows) != 2 {
		t.Fatalf("workflows: %+v", result.Workflows)
	}
	build, lint := result.Workflows[0], result.Workflows[1]
	if build.Outcome != OutcomeRerun || build.RerunID != "w3" || len(build.FailedJobs) != 1 || build.FailedJobs[0] != "jobb" ||
		build.RerunURL != "https://app.circleci.com/pipelines/github/o/r/12/workflows/w3" {
		t.Errorf("build: %+v", build)
	}
	if lint.Outcome != OutcomeNothing {
		t.Errorf("lint: %+v", lint)
	}
	if result.HeadSHA != "abc" || result.Pipeline.URL != "https://app.circleci.com/pipelines/github/o/r/12" {
		t.Errorf("pipeline: %+v sha %s", result.Pipeline, result.HeadSHA)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %q", warnings)
	}
}

// TestRunningIsRefused: nothing finished failed and a workflow still runs:
// exit 5 naming its state, no rerun.
func TestRunningIsRefused(t *testing.T) {
	result, _, server, err := run(t, sequence.Routes{
		workflowsPath: body(map[string]any{"items": []any{
			workflow("w1", "build", "failing", "2026-10-09T10:00:00Z"),
			workflow("w2", "lint", "success", "2026-10-09T10:00:00Z"),
		}}),
	})
	if code, verdict := agentcli.Outcome(err); code != agentcli.ExitRefused || verdict != agentcli.VerdictRefused || !strings.Contains(err.Error(), "build (failing)") {
		t.Fatalf("err: %v", err)
	}
	if len(posts(server)) != 0 || result.Workflows[0].Outcome != OutcomeRunning {
		t.Errorf("posts %q, workflows %+v", posts(server), result.Workflows)
	}
}

// TestRunningBesideARerunIsAWarning: a failed workflow is rerun and the one
// still running is named in a warning.
func TestRunningBesideARerunIsAWarning(t *testing.T) {
	_, warnings, _, err := run(t, sequence.Routes{
		workflowsPath: body(map[string]any{"items": []any{
			workflow("w1", "build", "failed", "2026-10-09T10:00:00Z"),
			workflow("w2", "e2e", "running", "2026-10-09T10:00:00Z"),
		}}),
		"GET /api/v2/workflow/w1/job":    jobs("failed"),
		"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w3"}}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "e2e (running)") {
		t.Errorf("warnings: %q", warnings)
	}
}

// TestNothingToRerun: green workflows and a failed one without a failed job
// are exit 3.
func TestNothingToRerun(t *testing.T) {
	result, _, server, err := run(t, sequence.Routes{
		workflowsPath: body(map[string]any{"items": []any{
			workflow("w1", "build", "success", "2026-10-09T10:00:00Z"),
			workflow("w2", "deploy", "canceled", "2026-10-09T10:00:00Z"),
		}}),
		"GET /api/v2/workflow/w2/job": jobs("not_run"),
	})
	if code, _ := agentcli.Outcome(err); code != agentcli.ExitNotApplicable {
		t.Fatalf("err: %v", err)
	}
	if len(posts(server)) != 0 || result.Workflows[1].Outcome != OutcomeNoFailedJob {
		t.Errorf("posts %q, workflows %+v", posts(server), result.Workflows)
	}
}

// TestFailedWithLaggingJobsIsRerun: a failed workflow whose job listing
// still reads blocked, as CircleCI's authenticated API answers for a while,
// is rerun all the same.
func TestFailedWithLaggingJobsIsRerun(t *testing.T) {
	result, _, server, err := run(t, sequence.Routes{
		workflowsPath:                    body(map[string]any{"items": []any{workflow("w1", "build", "failed", "2026-10-09T10:00:00Z")}}),
		"GET /api/v2/workflow/w1/job":    jobs("success", "blocked", "not_run"),
		"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusAccepted, Body: map[string]any{"workflow_id": "w2"}}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(posts(server)) != 1 || result.Workflows[0].Outcome != OutcomeRerun {
		t.Errorf("posts %q, workflows %+v", posts(server), result.Workflows)
	}
}

// TestForbiddenIsALogin: CircleCI's 403 to the rerun is exit 8 naming the
// CircleCI login, and the workflow is recorded as refused.
func TestForbiddenIsALogin(t *testing.T) {
	result, _, _, err := run(t, sequence.Routes{
		workflowsPath:                    body(map[string]any{"items": []any{workflow("w1", "build", "failed", "2026-10-09T10:00:00Z")}}),
		"GET /api/v2/workflow/w1/job":    jobs("failed"),
		"POST /api/v2/workflow/w1/rerun": {{Status: http.StatusForbidden, Body: map[string]any{"message": "Permission denied"}}},
	})
	if !errors.Is(err, authstore.ErrAuthRequired) || agentcli.Exit(err) != agentcli.ExitAuthRequired ||
		!strings.Contains(err.Error(), "devctl auth login --circleci-only") || !strings.Contains(err.Error(), "Write access") {
		t.Fatalf("err: %v", err)
	}
	if len(result.Workflows) != 1 || result.Workflows[0].Outcome != OutcomeRefused {
		t.Errorf("workflows: %+v", result.Workflows)
	}
}
