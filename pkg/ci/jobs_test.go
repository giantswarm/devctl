package ci

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// now is when every case reads CircleCI.
var now = time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)

func newClient(t *testing.T, routes sequence.Routes) (*circleciclient.Client, *circlemock.Server) {
	t.Helper()
	srv, err := circlemock.Start(routes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	client, err := circleciclient.New(circleciclient.Config{Token: "token_test", BaseURL: circleciclient.BaseURLFromAPIURL(srv.APIURL())})
	if err != nil {
		t.Fatal(err)
	}
	return client, srv
}

// stepOutput serves a step's output the way CircleCI's signed URLs do: one
// JSON array of messages with their times, per path.
func stepOutput(t *testing.T, outputs map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := outputs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The responses are shaped after CircleCI's. Pipeline 3885 of o/r built tag
// v1.2.3; its build workflow failed once (w2) and was rerun (w3), which is
// running: lint passed, push runs its "Push image" step, which last wrote a
// line at 17:24, test is blocked behind push.
func pipeline3885() map[string]any {
	return map[string]any{
		"id": "p-3885", "number": 3885, "state": "created", "project_slug": "gh/o/r",
		"created_at": "2026-10-09T17:00:00.000Z",
		"trigger":    map[string]any{"type": "webhook", "received_at": "2026-10-09T17:00:00.000Z"},
		"vcs":        map[string]any{"provider_name": "GitHub", "revision": "27b21bbfd09cf906d8ecce7d33ad37039f1d631b", "tag": "v1.2.3"},
	}
}

func workflows3885() map[string]any {
	return map[string]any{"items": []any{
		map[string]any{"id": "w3", "name": "build", "status": "running", "pipeline_id": "p-3885", "pipeline_number": 3885, "project_slug": "gh/o/r", "created_at": "2026-10-09T17:20:00Z", "stopped_at": nil},
		map[string]any{"id": "w2", "name": "build", "status": "failed", "pipeline_id": "p-3885", "pipeline_number": 3885, "project_slug": "gh/o/r", "created_at": "2026-10-09T17:01:00Z", "stopped_at": "2026-10-09T17:10:00Z"},
		map[string]any{"id": "w1", "name": "setup", "status": "success", "tag": "setup", "pipeline_id": "p-3885", "pipeline_number": 3885, "project_slug": "gh/o/r", "created_at": "2026-10-09T17:00:00Z", "stopped_at": "2026-10-09T17:01:00Z"},
	}, "next_page_token": nil}
}

func jobsW3() map[string]any {
	return map[string]any{"items": []any{
		map[string]any{"id": "j-lint", "name": "lint", "type": "build", "status": "success", "job_number": 3, "started_at": "2026-10-09T17:20:10Z", "stopped_at": "2026-10-09T17:22:10Z", "dependencies": []any{}},
		map[string]any{"id": "j-push", "name": "push", "type": "build", "status": "running", "job_number": 4, "started_at": "2026-10-09T17:22:30Z", "stopped_at": nil, "dependencies": []any{"j-lint"}},
		map[string]any{"id": "j-test", "name": "test", "type": "build", "status": "blocked", "started_at": nil, "stopped_at": nil, "dependencies": []any{"j-push"}},
	}, "next_page_token": nil}
}

func job4Steps(outputURL string) map[string]any {
	return map[string]any{
		"build_num": 4, "status": "running", "lifecycle": "running",
		"steps": []any{
			map[string]any{"name": "Spin up environment", "actions": []any{
				map[string]any{"name": "Spin up environment", "index": 0, "status": "success", "start_time": "2026-10-09T17:22:35.000Z", "end_time": "2026-10-09T17:22:40.000Z", "output_url": outputURL + "/spin", "has_output": true},
			}},
			map[string]any{"name": "Push image", "actions": []any{
				map[string]any{"name": "Push image", "index": 0, "status": "running", "start_time": "2026-10-09T17:23:00.000Z", "end_time": nil, "output_url": outputURL + "/push", "has_output": true},
			}},
		},
	}
}

const pushOutput = `[{"message":"Pushing layer 1/4\n","time":"2026-10-09T17:23:05.000Z","type":"out","truncated":false},` +
	`{"message":"Pushing layer 2/4\n","time":"2026-10-09T17:24:00.000Z","type":"out","truncated":false}]`

func routes3885(outputURL string) sequence.Routes {
	return sequence.Routes{
		"GET /api/v2/project/gh/o/r/pipeline/3885": {{Body: pipeline3885()}},
		"GET /api/v2/pipeline/p-3885":              {{Body: pipeline3885()}},
		"GET /api/v2/pipeline/p-3885/workflow":     {{Body: workflows3885()}},
		"GET /api/v2/workflow/w3/job":              {{Body: jobsW3()}},
		"GET /api/v2/workflow/w2/job":              {{Body: map[string]any{"items": []any{map[string]any{"id": "j-old", "name": "lint", "type": "build", "status": "failed", "job_number": 2, "started_at": "2026-10-09T17:01:10Z", "stopped_at": "2026-10-09T17:10:00Z"}}}}},
		"GET /api/v2/workflow/w1/job":              {{Body: map[string]any{"items": []any{map[string]any{"id": "j-setup", "name": "setup", "type": "build", "status": "success", "job_number": 1, "started_at": "2026-10-09T17:00:05Z", "stopped_at": "2026-10-09T17:00:55Z"}}}}},
		"GET /api/v1.1/project/github/o/r/4":       {{Body: job4Steps(outputURL)}},
	}
}

func jobs(t *testing.T, client *circleciclient.Client, pipeline string) (*JobsResult, []string, error) {
	t.Helper()
	result := NewJobsResult("o/r")
	var warnings []string
	err := Jobs(context.Background(), client, "o", "r", pipeline, now, result, func(m string) { warnings = append(warnings, m) })
	return result, warnings, err
}

func workflow(t *testing.T, result *JobsResult, id string) Workflow {
	t.Helper()
	for _, w := range result.Workflows {
		if w.ID == id {
			return w
		}
	}
	t.Fatalf("no workflow %s in %+v", id, result.Workflows)
	return Workflow{}
}

func job(t *testing.T, w Workflow, name string) Job {
	t.Helper()
	for _, j := range w.Jobs {
		if j.Name == name {
			return j
		}
	}
	t.Fatalf("no job %s in %+v", name, w.Jobs)
	return Job{}
}

// TestJobsReadsEveryRunAndJob: the pipeline by its number, every workflow
// run with the newest of a name marked latest, every job with its times,
// and for the running job the step it is in and the age of its last output.
func TestJobsReadsEveryRunAndJob(t *testing.T) {
	output := stepOutput(t, map[string]string{"/push": pushOutput, "/spin": `[]`})
	client, _ := newClient(t, routes3885(output.URL))
	result, warnings, err := jobs(t, client, "3885")
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %v", warnings)
	}
	if result.Pipeline == nil || result.Pipeline.Number != 3885 || result.Pipeline.Tag != "v1.2.3" || result.Pipeline.State != "created" || result.Pipeline.URL != "https://app.circleci.com/pipelines/github/o/r/3885" {
		t.Errorf("pipeline: %+v", result.Pipeline)
	}
	if len(result.Workflows) != 3 || result.Workflows[0].ID != "w2" || result.Workflows[1].ID != "w3" || result.Workflows[2].ID != "w1" {
		t.Fatalf("workflows: want w2, w3, w1 by name then age, got %+v", result.Workflows)
	}
	old, latest, setup := workflow(t, result, "w2"), workflow(t, result, "w3"), workflow(t, result, "w1")
	if old.Latest || !latest.Latest || !setup.Latest {
		t.Errorf("latest: w2 %v, w3 %v, w1 %v; want the newest run of a name only", old.Latest, latest.Latest, setup.Latest)
	}
	if old.StoppedAt == nil || latest.StoppedAt != nil || latest.Status != "running" || latest.URL != "https://app.circleci.com/pipelines/github/o/r/3885/workflows/w3" {
		t.Errorf("runs: old %+v, latest %+v", old.Run, latest.Run)
	}

	lint := job(t, latest, "lint")
	if lint.Number != 3 || lint.DurationSeconds != 120 || lint.StartedAt == nil || lint.StoppedAt == nil || lint.Step != nil {
		t.Errorf("lint: %+v", lint)
	}
	push := job(t, latest, "push")
	if push.Number != 4 || push.Status != "running" || push.DurationSeconds != 2250 || push.StoppedAt != nil {
		t.Errorf("push: %+v", push)
	}
	if push.Step == nil {
		t.Fatalf("push has no step: %+v", push)
	}
	if push.Step.Name != "Push image" || push.Step.RunningSeconds != 2220 || push.Step.EndedAt != nil {
		t.Errorf("push step: %+v", push.Step)
	}
	if push.Step.LastOutputAt == nil || !push.Step.LastOutputAt.Equal(time.Date(2026, 10, 9, 17, 24, 0, 0, time.UTC)) || push.Step.OutputAgeSeconds != 2160 {
		t.Errorf("push output: %+v", push.Step)
	}
	test := job(t, latest, "test")
	if test.Number != 0 || test.StartedAt != nil || test.DurationSeconds != 0 || test.Step != nil {
		t.Errorf("test: %+v", test)
	}
}

// TestJobsByIDChecksTheProject: a pipeline named by its id is read from the
// pipeline endpoint and has to belong to the repository.
func TestJobsByIDChecksTheProject(t *testing.T) {
	output := stepOutput(t, map[string]string{"/push": pushOutput})
	client, srv := newClient(t, routes3885(output.URL))
	result, _, err := jobs(t, client, "p-3885")
	if err != nil || len(result.Workflows) != 3 {
		t.Fatalf("by id: err=%v workflows=%d", err, len(result.Workflows))
	}
	for _, r := range srv.Requests() {
		if strings.Contains(r.Path, "/project/gh/o/r/pipeline/") {
			t.Errorf("the project pipeline endpoint was read for an id: %v", r)
		}
	}

	other := pipeline3885()
	other["project_slug"] = "gh/o/other"
	client, srv = newClient(t, sequence.Routes{"GET /api/v2/pipeline/p-3885": {{Body: other}}})
	result, _, err = jobs(t, client, "p-3885")
	if agentcli.Exit(err) != agentcli.ExitNotApplicable || result.Pipeline != nil {
		t.Errorf("another project's pipeline: err=%v pipeline=%+v, want exit 3 and no pipeline", err, result.Pipeline)
	}
	if len(srv.Requests()) != 1 {
		t.Errorf("requests after the project check: %v", srv.Requests())
	}
}

// TestJobsUnknownPipelineIsNotApplicable: CircleCI answers 404: exit 3.
func TestJobsUnknownPipelineIsNotApplicable(t *testing.T) {
	client, _ := newClient(t, sequence.Routes{})
	if _, _, err := jobs(t, client, "99"); agentcli.Exit(err) != agentcli.ExitNotApplicable {
		t.Errorf("unknown number: %v, want exit 3", err)
	}
	if _, _, err := jobs(t, client, "p-unknown"); agentcli.Exit(err) != agentcli.ExitNotApplicable {
		t.Errorf("unknown id: %v, want exit 3", err)
	}
}

// TestJobsLaggingAnswersAreWarnings: jobs CircleCI does not list yet, steps
// it does not answer and output it does not serve are warnings; the rest of
// the view is read.
func TestJobsLaggingAnswersAreWarnings(t *testing.T) {
	output := stepOutput(t, map[string]string{})
	routes := routes3885(output.URL)
	delete(routes, "GET /api/v2/workflow/w1/job")
	delete(routes, "GET /api/v1.1/project/github/o/r/4")
	client, _ := newClient(t, routes)
	result, warnings, err := jobs(t, client, "3885")
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], "steps of job push (4)") || !strings.Contains(warnings[1], "jobs of workflow setup (w1)") {
		t.Errorf("warnings: %v", warnings)
	}
	if push := job(t, workflow(t, result, "w3"), "push"); push.Step != nil {
		t.Errorf("push has a step without its steps answered: %+v", push.Step)
	}
	if setup := workflow(t, result, "w1"); len(setup.Jobs) != 0 {
		t.Errorf("setup has jobs without a listing: %+v", setup.Jobs)
	}

	routes = routes3885(output.URL)
	client, _ = newClient(t, routes)
	result, warnings, err = jobs(t, client, "3885")
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}
	push := job(t, workflow(t, result, "w3"), "push")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "output of step \"Push image\"") || push.Step == nil || push.Step.LastOutputAt != nil || push.Step.Name != "Push image" {
		t.Errorf("unserved output: warnings=%v step=%+v, want the step without its output and one warning", warnings, push.Step)
	}
}
