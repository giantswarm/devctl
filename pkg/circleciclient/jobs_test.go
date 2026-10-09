package circleciclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// recorder serves body for every request and keeps the last request's
// method, path and JSON body.
type recorder struct {
	method, path string
	body         map[string]any
	hasBody      bool
	status       int
	answer       string
}

func (r *recorder) serve(t *testing.T) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.method, r.path = req.Method, req.URL.Path
		r.body, r.hasBody = nil, false
		if req.ContentLength != 0 {
			r.hasBody = true
			_ = json.NewDecoder(req.Body).Decode(&r.body)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.status != 0 {
			w.WriteHeader(r.status)
		}
		_, _ = w.Write([]byte(r.answer))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return c, srv
}

// TestRerunWorkflowPostsFromFailed: the rerun is one POST to the workflow
// carrying from_failed as asked, in full or from failed.
func TestRerunWorkflowPostsFromFailed(t *testing.T) {
	r := &recorder{status: http.StatusAccepted, answer: `{"workflow_id": "w2"}`}
	c, _ := r.serve(t)
	for _, fromFailed := range []bool{false, true} {
		id, err := c.RerunWorkflow(context.Background(), "w1", fromFailed)
		if err != nil || id != "w2" {
			t.Fatalf("from_failed=%v: id=%q err=%v", fromFailed, id, err)
		}
		if r.method != http.MethodPost || r.path != "/api/v2/workflow/w1/rerun" || len(r.body) != 1 || r.body["from_failed"] != fromFailed {
			t.Errorf("from_failed=%v: request %s %s %v", fromFailed, r.method, r.path, r.body)
		}
	}
}

// TestCancelWorkflowPosts: the cancel is one POST without a body; a 403 is
// IsForbidden.
func TestCancelWorkflowPosts(t *testing.T) {
	r := &recorder{status: http.StatusAccepted, answer: `{"message": "Accepted."}`}
	c, _ := r.serve(t)
	if err := c.CancelWorkflow(context.Background(), "w1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if r.method != http.MethodPost || r.path != "/api/v2/workflow/w1/cancel" || r.hasBody {
		t.Errorf("request: %s %s body=%v", r.method, r.path, r.hasBody)
	}
	r.status, r.answer = http.StatusForbidden, `{"message": "Permission denied"}`
	if err := c.CancelWorkflow(context.Background(), "w1"); !IsForbidden(err) {
		t.Errorf("a 403: want IsForbidden, got %v", err)
	}
}

// TestGetPipelineAndWorkflowPaths: a pipeline by number is read under the
// project, one by id and a workflow under their own endpoints, each with the
// project it belongs to.
func TestGetPipelineAndWorkflowPaths(t *testing.T) {
	r := &recorder{answer: `{"id": "p1", "number": 12, "state": "created", "project_slug": "gh/o/r", "vcs": {"tag": "v1.0.0"}}`}
	c, _ := r.serve(t)
	p, err := c.GetProjectPipeline(context.Background(), "o", "r", 12)
	if err != nil || p.Number != 12 || p.ProjectSlug != "gh/o/r" || p.VCS.Tag != "v1.0.0" {
		t.Fatalf("by number: %+v %v", p, err)
	}
	if r.method != http.MethodGet || r.path != "/api/v2/project/gh/o/r/pipeline/12" {
		t.Errorf("by number: %s %s", r.method, r.path)
	}
	if _, err := c.GetPipeline(context.Background(), "p1"); err != nil || r.path != "/api/v2/pipeline/p1" {
		t.Errorf("by id: %s %v", r.path, err)
	}

	r.answer = `{"id": "w1", "name": "build", "status": "running", "created_at": "2026-10-09T17:20:00Z", "stopped_at": null, "pipeline_id": "p1", "pipeline_number": 12, "project_slug": "gh/o/r"}`
	w, err := c.GetWorkflow(context.Background(), "w1")
	if err != nil || w.Name != "build" || w.PipelineID != "p1" || w.PipelineNumber != 12 || w.ProjectSlug != "gh/o/r" || !w.StoppedAt.IsZero() {
		t.Fatalf("workflow: %+v %v", w, err)
	}
	if r.path != "/api/v2/workflow/w1" {
		t.Errorf("workflow: %s", r.path)
	}
	if ProjectSlug("o", "r") != "gh/o/r" {
		t.Errorf("ProjectSlug: %s", ProjectSlug("o", "r"))
	}
}

// TestJobStepsFlattensActions: the v1.1 job's steps are read in order, a
// parallel step once per action, with their times and output URLs.
func TestJobStepsFlattensActions(t *testing.T) {
	r := &recorder{answer: `{"build_num": 4, "steps": [
		{"name": "Checkout code", "actions": [{"name": "Checkout code", "index": 0, "status": "success", "start_time": "2026-10-09T17:22:35.000Z", "end_time": "2026-10-09T17:22:40.000Z", "output_url": "https://out/checkout", "has_output": true}]},
		{"name": "Run tests", "actions": [
			{"name": "Run tests", "index": 0, "status": "running", "start_time": "2026-10-09T17:23:00.000Z", "end_time": null, "output_url": "https://out/tests-0"},
			{"name": "Run tests", "index": 1, "status": "running", "start_time": "2026-10-09T17:23:01.000Z", "end_time": null, "output_url": "https://out/tests-1"}]},
		{"name": "Upload", "actions": [{"name": "Upload", "index": 0, "status": "not_run", "start_time": null, "end_time": null, "has_output": false}]}
	]}`}
	c, _ := r.serve(t)
	steps, err := c.JobSteps(context.Background(), "o", "r", 4)
	if err != nil {
		t.Fatalf("steps: %v", err)
	}
	if r.path != "/api/v1.1/project/github/o/r/4" {
		t.Errorf("path: %s", r.path)
	}
	if len(steps) != 4 || steps[0].Name != "Checkout code" || steps[0].EndedAt.IsZero() || steps[1].Index != 0 || steps[2].Index != 1 || !steps[2].EndedAt.IsZero() || steps[2].OutputURL != "https://out/tests-1" || !steps[3].StartedAt.IsZero() || steps[3].OutputURL != "" {
		t.Errorf("steps: %+v", steps)
	}
	if want := time.Date(2026, 10, 9, 17, 23, 1, 0, time.UTC); !steps[2].StartedAt.Equal(want) {
		t.Errorf("parallel action start: %v, want %v", steps[2].StartedAt, want)
	}
}

// TestLastOutputAtKeepsTheLatestTime: the output's messages are read for
// their times only; an empty output is a zero time; a non-JSON answer and a
// non-200 status are errors.
func TestLastOutputAtKeepsTheLatestTime(t *testing.T) {
	r := &recorder{answer: `[{"message": "a\n", "time": "2026-10-09T17:23:05.000Z", "type": "out"}, {"message": "b\n", "time": "2026-10-09T17:24:00.000Z", "type": "out"}, {"message": "c\n", "time": "2026-10-09T17:23:30.000Z", "type": "err"}]`}
	c, srv := r.serve(t)
	last, err := c.LastOutputAt(context.Background(), srv.URL+"/output")
	if err != nil || !last.Equal(time.Date(2026, 10, 9, 17, 24, 0, 0, time.UTC)) {
		t.Errorf("last: %v %v", last, err)
	}
	r.answer = `[]`
	if last, err := c.LastOutputAt(context.Background(), srv.URL+"/output"); err != nil || !last.IsZero() {
		t.Errorf("empty: %v %v, want zero", last, err)
	}
	r.answer = `not json`
	if _, err := c.LastOutputAt(context.Background(), srv.URL+"/output"); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("not JSON: %v", err)
	}
	r.status, r.answer = http.StatusForbidden, `expired`
	if _, err := c.LastOutputAt(context.Background(), srv.URL+"/output"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("403: %v", err)
	}
}
