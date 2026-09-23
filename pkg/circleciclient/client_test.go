package circleciclient

import (
	"context"
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
