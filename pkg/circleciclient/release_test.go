package circleciclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBaseURLFromAPIURL(t *testing.T) {
	cases := map[string]string{
		"https://circleci.com/api/v2":  "https://circleci.com",
		"https://circleci.com/api/v2/": "https://circleci.com",
		"http://127.0.0.1:4567/api/v2": "http://127.0.0.1:4567",
		"https://circleci.com":         "https://circleci.com",
	}
	for in, want := range cases {
		if got := BaseURLFromAPIURL(in); got != want {
			t.Errorf("%s: want %s, got %s", in, want, got)
		}
	}
}

func TestNewestWorkflows(t *testing.T) {
	t0 := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	runs := []Workflow{
		{ID: "w1", Name: "build", Status: "failed", CreatedAt: t0},
		{ID: "w3", Name: "setup", Status: "success", CreatedAt: t0.Add(-time.Minute)},
		{ID: "w2", Name: "build", Status: "success", CreatedAt: t0.Add(20 * time.Minute)},
	}
	newest := NewestWorkflows(runs)
	if len(newest) != 2 || newest[0].ID != "w2" || newest[0].Status != "success" || newest[1].ID != "w3" {
		t.Errorf("newest: %+v", newest)
	}
}

func TestFindPipelineByTagPages(t *testing.T) {
	pages := map[string]PipelinePage{
		"": {Items: []Pipeline{{ID: "p3", Number: 3, VCS: PipelineVCS{Branch: "main"}}}, NextPageToken: "two"},
		"two": {Items: []Pipeline{
			{ID: "p2", Number: 2, VCS: PipelineVCS{Tag: "v1.2.3", Revision: "abc"}},
			{ID: "p1", Number: 1, VCS: PipelineVCS{Tag: "v1.2.2"}},
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/project/gh/giantswarm/devctl/pipeline" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(pages[r.URL.Query().Get("page-token")])
	}))
	defer server.Close()

	client, err := New(Config{Token: "t", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p, err := client.FindPipelineByTag(context.Background(), "giantswarm", "devctl", "v1.2.3")
	if err != nil {
		t.Fatalf("FindPipelineByTag: %v", err)
	}
	if p == nil || p.ID != "p2" {
		t.Errorf("pipeline: %+v", p)
	}
	none, err := client.FindPipelineByTag(context.Background(), "giantswarm", "devctl", "v9.9.9")
	if err != nil || none != nil {
		t.Errorf("unknown tag: %+v %v", none, err)
	}
	if got := PipelineURL("giantswarm", "devctl", 2); got != "https://app.circleci.com/pipelines/github/giantswarm/devctl/2" {
		t.Errorf("url: %s", got)
	}
}

func TestParseStatusTarget(t *testing.T) {
	cases := map[string]struct {
		want StatusTarget
		ok   bool
	}{
		"https://circleci.com/gh/giantswarm/backstage/60975":                                       {StatusTarget{Org: "giantswarm", Repo: "backstage", Job: 60975}, true},
		"https://app.circleci.com/pipelines/github/giantswarm/backstage/4211":                      {StatusTarget{Org: "giantswarm", Repo: "backstage", Pipeline: 4211}, true},
		"https://app.circleci.com/pipelines/gh/giantswarm/backstage/4211/workflows/w-1/jobs/60975": {StatusTarget{Org: "giantswarm", Repo: "backstage", Pipeline: 4211, Job: 60975}, true},
		"https://app.circleci.com/pipelines/github/giantswarm/backstage/4211/workflows/w-1":        {StatusTarget{Org: "giantswarm", Repo: "backstage", Pipeline: 4211}, true},
		"https://circleci.com/gh/giantswarm/backstage":                                             {},
		"https://circleci.com/gh/giantswarm/backstage/latest":                                      {},
		"https://github.com/giantswarm/backstage/actions/runs/1":                                   {},
		"": {},
	}
	for in, c := range cases {
		got, ok := ParseStatusTarget(in)
		if ok != c.ok || got != c.want {
			t.Errorf("%q: want %+v %v, got %+v %v", in, c.want, c.ok, got, ok)
		}
	}
}
