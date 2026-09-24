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
