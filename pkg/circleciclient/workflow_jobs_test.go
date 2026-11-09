package circleciclient_test

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/ci"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

type jobPageTransport func(*http.Request) (*http.Response, error)

func (f jobPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestListWorkflowJobsReadsEveryPage(t *testing.T) {
	const next = "cursor +/=&?"
	var tokens []string
	c, err := circleciclient.New(circleciclient.Config{
		Token:   "cci_test",
		BaseURL: "https://circleci.invalid",
		Transport: jobPageTransport(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || req.URL.Path != "/api/v2/workflow/w1/job" {
				t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			}
			if req.Header.Get("Circle-Token") != "cci_test" {
				t.Fatal("every page must use the selected identity")
			}
			token := req.URL.Query().Get("page-token")
			tokens = append(tokens, token)
			var body string
			switch len(tokens) {
			case 1:
				body = `{"items":[{"id":"mirror","name":"sync-china-registry","status":"failed"}],"next_page_token":"cursor +/=&?"}`
			case 2:
				if token != next {
					t.Fatalf("continuation token: got %q, want %q", token, next)
				}
				// An empty page is not the end when it carries a continuation.
				body = `{"items":[],"next_page_token":"last"}`
			case 3:
				body = `{"items":[{"id":"publish","name":"push-to-registries-release","status":"failed","job_number":42}],"next_page_token":null}`
			default:
				t.Fatal("requested a page after the final page")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	jobs, err := c.ListWorkflowJobs(context.Background(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"", next, "last"}) {
		t.Fatalf("pages read: %q", tokens)
	}
	if len(jobs) != 2 || jobs[1].ID != "publish" || jobs[1].JobNumber != 42 || !circleciclient.JobFailed(jobs[1].Status) {
		t.Fatalf("required failed publish lost after advisory failure: %+v", jobs)
	}
}

func TestListWorkflowJobsRejectsPartialResults(t *testing.T) {
	testCases := []struct {
		name   string
		cancel bool
	}{
		{name: "case 0: later page refused"},
		{name: "case 1: canceled between pages", cancel: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requests int
			c, err := circleciclient.New(circleciclient.Config{
				Anonymous: true,
				BaseURL:   "https://circleci.invalid",
				Transport: jobPageTransport(func(req *http.Request) (*http.Response, error) {
					requests++
					if requests > 2 {
						t.Fatal("retried a refused page")
					}
					if err := req.Context().Err(); err != nil {
						return nil, err
					}
					code, body := http.StatusOK, `{"items":[{"id":"mirror","status":"failed"}],"next_page_token":"two"}`
					if requests == 2 {
						code, body = http.StatusServiceUnavailable, `{"message":"try again"}`
					} else if tc.cancel {
						cancel()
					}
					return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			jobs, err := c.ListWorkflowJobs(ctx, "w1")
			if err == nil || jobs != nil {
				t.Fatalf("partial listing must fail: jobs=%+v err=%v", jobs, err)
			}
			if !tc.cancel && (!circleciclient.IsAPI(err) || requests != 2) {
				t.Fatalf("later-page error: requests=%d err=%v", requests, err)
			}
		})
	}
}

func TestResolveJobURLFindsLaterPageJob(t *testing.T) {
	c, err := circleciclient.New(circleciclient.Config{
		Anonymous: true,
		BaseURL:   "https://circleci.invalid",
		Transport: jobPageTransport(func(req *http.Request) (*http.Response, error) {
			var body string
			switch req.URL.Path {
			case "/api/v2/workflow/w1":
				body = `{"id":"w1","project_slug":"gh/o/r"}`
			case "/api/v2/workflow/w1/job":
				if req.URL.Query().Get("page-token") == "" {
					body = `{"items":[{"id":"first","job_number":1}],"next_page_token":"two"}`
				} else {
					body = `{"items":[{"id":"publish","job_number":42}],"next_page_token":null}`
				}
			default:
				t.Fatalf("unexpected request: %s", req.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, repo, number, ok, err := ci.ResolveJobURL(context.Background(), c, "https://app.circleci.com/workflow/w1/job/publish")
	if err != nil || !ok || owner != "o" || repo != "r" || number != "42" {
		t.Fatalf("resolve later-page job: %q %q %q %v %v", owner, repo, number, ok, err)
	}
}
