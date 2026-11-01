package circleciclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAnonymousClientSendsNoToken: a client without a token reads a public
// project's pipelines and sends no Circle-Token header; a client with a
// token sends it.
func TestAnonymousClientSendsNoToken(t *testing.T) {
	var headers []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = append(headers, r.Header.Get("Circle-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": [{"id": "p1", "number": 7, "vcs": {"tag": "v1.0.0"}}], "next_page_token": null}`))
	}))
	defer srv.Close()

	anon, err := New(Config{Anonymous: true, BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("anonymous client: %v", err)
	}
	page, err := anon.ListPipelines(context.Background(), "giantswarm", "backstage", "")
	if err != nil || len(page.Items) != 1 || page.Items[0].VCS.Tag != "v1.0.0" {
		t.Fatalf("anonymous list: page=%+v err=%v", page, err)
	}
	withToken, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("token client: %v", err)
	}
	if _, err := withToken.ListPipelines(context.Background(), "giantswarm", "backstage", ""); err != nil {
		t.Fatalf("token list: %v", err)
	}
	if len(headers) != 2 || headers[0] != "" || headers[1] != "cci_test" {
		t.Errorf("Circle-Token headers sent: %q, want none then the token", headers)
	}
}

// TestTriggerTagPipelinePostsTheTag: the trigger is one POST to the
// project's pipelines naming the tag, and the pipeline it answers carries
// the tag; an empty tag is refused before any request.
func TestTriggerTagPipelinePostsTheTag(t *testing.T) {
	var method, path string
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": "p2", "number": 2, "state": "setup-pending"}`))
	}))
	defer srv.Close()

	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	p, err := c.TriggerTagPipeline(context.Background(), "giantswarm", "sample-service", "v0.1.0")
	if err != nil || p.Number != 2 || p.VCS.Tag != "v0.1.0" {
		t.Fatalf("trigger: pipeline=%+v err=%v", p, err)
	}
	if method != http.MethodPost || path != "/api/v2/project/gh/giantswarm/sample-service/pipeline" || len(body) != 1 || body["tag"] != "v0.1.0" {
		t.Errorf("request: %s %s %v, want POST of the tag alone to the project's pipelines", method, path, body)
	}
	method = ""
	if _, err := c.TriggerTagPipeline(context.Background(), "giantswarm", "sample-service", ""); !IsInvalidConfig(err) || method != "" {
		t.Errorf("an empty tag: err=%v, request %q; want an invalid config error and no request", err, method)
	}
}

// TestNewRefusesTheWrongTokenShape: neither a token nor Anonymous is a
// config error, as is both.
func TestNewRefusesTheWrongTokenShape(t *testing.T) {
	if _, err := New(Config{}); !IsInvalidConfig(err) {
		t.Errorf("no token, not anonymous: want an invalid config error, got %v", err)
	}
	if _, err := New(Config{Token: "cci_test", Anonymous: true}); !IsInvalidConfig(err) {
		t.Errorf("a token and anonymous: want an invalid config error, got %v", err)
	}
}

func TestWorkflowURL(t *testing.T) {
	got := WorkflowURL("giantswarm", "backstage", 11508, "217ae45f-d1ca-4347-9f4e-2bd4e2bd550a")
	want := "https://app.circleci.com/pipelines/github/giantswarm/backstage/11508/workflows/217ae45f-d1ca-4347-9f4e-2bd4e2bd550a"
	if got != want {
		t.Errorf("WorkflowURL: got %s, want %s", got, want)
	}
}

// TestRerunWorkflowFromFailed: the rerun is one POST to the workflow with
// from_failed, answering the new workflow's id; a 403 is IsForbidden.
func TestRerunWorkflowFromFailed(t *testing.T) {
	var method, path string
	var body map[string]any
	status := http.StatusAccepted
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusForbidden {
			_, _ = w.Write([]byte(`{"message": "Permission denied"}`))
			return
		}
		_, _ = w.Write([]byte(`{"workflow_id": "w2"}`))
	}))
	defer srv.Close()

	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	id, err := c.RerunWorkflowFromFailed(context.Background(), "w1")
	if err != nil || id != "w2" {
		t.Fatalf("rerun: id=%q err=%v", id, err)
	}
	if method != http.MethodPost || path != "/api/v2/workflow/w1/rerun" || len(body) != 1 || body["from_failed"] != true {
		t.Errorf("request: %s %s %v, want POST of from_failed alone to the workflow's rerun", method, path, body)
	}
	status = http.StatusForbidden
	if _, err := c.RerunWorkflowFromFailed(context.Background(), "w1"); !IsForbidden(err) {
		t.Errorf("a 403: want IsForbidden, got %v", err)
	}
}

// TestFindPipelineByRevision: the branch's pages are read newest first until
// the revision's pipeline turns up, and no further than RevisionPipelinePages.
func TestFindPipelineByRevision(t *testing.T) {
	pages := map[string]string{
		"":   `{"items": [{"id": "p3", "vcs": {"revision": "c"}}], "next_page_token": "t2"}`,
		"t2": `{"items": [{"id": "p2", "vcs": {"revision": "b"}}], "next_page_token": "t3"}`,
		"t3": `{"items": [{"id": "p1", "vcs": {"revision": "a"}}], "next_page_token": "t4"}`,
		"t4": `{"items": [{"id": "p0", "vcs": {"revision": "z"}}], "next_page_token": ""}`,
	}
	var reads []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("branch") != "feature" {
			t.Errorf("branch: got %q, want feature", r.URL.Query().Get("branch"))
		}
		token := r.URL.Query().Get("page-token")
		reads = append(reads, token)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pages[token]))
	}))
	defer srv.Close()

	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	p, err := c.FindPipelineByRevision(context.Background(), "o", "r", "feature", "b")
	if err != nil || p == nil || p.ID != "p2" {
		t.Fatalf("revision b: pipeline=%+v err=%v", p, err)
	}
	reads = nil
	p, err = c.FindPipelineByRevision(context.Background(), "o", "r", "feature", "z")
	if err != nil || p != nil || len(reads) != RevisionPipelinePages {
		t.Errorf("revision z past the bound: pipeline=%+v err=%v reads=%q, want nil after %d pages", p, err, reads, RevisionPipelinePages)
	}
}

// TestPipeline_githubAppTrigger: a pipeline the CircleCI GitHub App triggered
// carries no vcs; what it built is read from trigger_parameters.git.
func TestPipeline_githubAppTrigger(t *testing.T) {
	tests := []struct {
		name string
		body string
		want PipelineVCS
	}{
		{
			name: "oauth webhook keeps vcs",
			body: `{"id": "p1", "vcs": {"revision": "a", "branch": "main"}, "trigger_parameters": {"git": {"checkout_sha": "other", "branch": "other"}}}`,
			want: PipelineVCS{Revision: "a", Branch: "main"},
		},
		{
			name: "github app branch push",
			body: `{"id": "p2", "vcs": null, "trigger_parameters": {"git": {"checkout_sha": "b", "branch": "feature", "ref": "refs/heads/feature"}}}`,
			want: PipelineVCS{Revision: "b", Branch: "feature"},
		},
		{
			name: "github app tag push",
			body: `{"id": "p3", "trigger_parameters": {"git": {"checkout_sha": "c", "branch": "main", "ref": "refs/tags/v1.2.3"}}}`,
			want: PipelineVCS{Revision: "c", Tag: "v1.2.3"},
		},
		{
			name: "github app empty vcs and branch only in ref",
			body: `{"id": "p4", "vcs": {}, "trigger_parameters": {"git": {"checkout_sha": "d", "ref": "refs/heads/feature"}}}`,
			want: PipelineVCS{Revision: "d", Branch: "feature"},
		},
		{
			name: "api parameter named git is ignored",
			body: `{"id": "p5", "trigger_parameters": {"git": "yes"}}`,
			want: PipelineVCS{},
		},
		{
			name: "mistyped git fields are ignored",
			body: `{"id": "p6", "trigger_parameters": {"git": {"checkout_sha": 7, "branch": true}}}`,
			want: PipelineVCS{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Pipeline
			if err := json.Unmarshal([]byte(tt.body), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if p.VCS != tt.want {
				t.Errorf("vcs: got %+v, want %+v", p.VCS, tt.want)
			}
		})
	}
}

// TestFindPipelineByRevision_githubAppTrigger: the revision's pipeline is
// found when the GitHub App triggered it, as on a project CircleCI builds
// through its GitHub App.
func TestFindPipelineByRevision_githubAppTrigger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": [{"id": "p2", "vcs": null, "trigger_parameters": {"git": {"checkout_sha": "b", "branch": "feature"}}}], "next_page_token": null}`))
	}))
	defer srv.Close()

	c, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	p, err := c.FindPipelineByRevision(context.Background(), "o", "r", "feature", "b")
	if err != nil || p == nil || p.ID != "p2" {
		t.Fatalf("revision b: pipeline=%+v err=%v", p, err)
	}
}

func TestPullRequestBranch(t *testing.T) {
	if got := PullRequestBranch("feature", false, 7); got != "feature" {
		t.Errorf("own branch: got %q", got)
	}
	if got := PullRequestBranch("feature", true, 7); got != "pull/7" {
		t.Errorf("fork: got %q, want pull/7", got)
	}
}
