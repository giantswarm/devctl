package circleciclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestJobReadsPublicFallback(t *testing.T) {
	testCases := []struct {
		name       string
		token      string
		status     int
		publicCode int
		wantCalls  int
	}{
		{name: "case 0: authenticated success", token: "cci_test", status: 200, publicCode: 200, wantCalls: 1},
		{name: "case 1: authenticated not found but public", token: "cci_test", status: 404, publicCode: 200, wantCalls: 2},
		{name: "case 2: private or missing", token: "cci_test", status: 404, publicCode: 404, wantCalls: 2},
		{name: "case 3: unauthorized", token: "cci_test", status: 401, publicCode: 200, wantCalls: 1},
		{name: "case 4: forbidden", token: "cci_test", status: 403, publicCode: 200, wantCalls: 1},
		{name: "case 5: unavailable", token: "cci_test", status: 503, publicCode: 200, wantCalls: 1},
		{name: "case 6: anonymous success", status: 200, publicCode: 200, wantCalls: 1},
		{name: "case 7: anonymous missing", status: 404, publicCode: 404, wantCalls: 1},
		{name: "case 8: public refusal", token: "cci_test", status: 404, publicCode: 403, wantCalls: 2},
	}
	for _, tc := range testCases {
		for _, path := range []string{"/api/v2/workflow/continued/job", "/api/v1.1/project/github/o/r/99"} {
			t.Run(tc.name+path, func(t *testing.T) {
				var headers []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != path {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					}
					token := r.Header.Get("Circle-Token")
					headers = append(headers, token)
					code := tc.status
					if len(headers) > 1 {
						code = tc.publicCode
					}
					w.WriteHeader(code)
					if code != http.StatusOK {
						_, _ = fmt.Fprint(w, `{"message":"not readable"}`)
						return
					}
					if path == "/api/v2/workflow/continued/job" {
						_, _ = fmt.Fprint(w, `{"items":[{"name":"go-test","status":"failed","job_number":99}]}`)
					} else {
						_, _ = fmt.Fprint(w, `{"steps":[]}`)
					}
				}))
				defer server.Close()
				client, err := New(Config{Token: tc.token, Anonymous: tc.token == "", BaseURL: server.URL})
				if err != nil {
					t.Fatal(err)
				}
				if path == "/api/v2/workflow/continued/job" {
					var jobs []Job
					jobs, err = client.ListWorkflowJobs(context.Background(), "continued")
					if err == nil && (len(jobs) != 1 || jobs[0].JobNumber != 99) {
						t.Errorf("jobs = %+v, want continued job 99", jobs)
					}
				} else {
					_, err = client.FailedStepsOutput(context.Background(), "o", "r", 99)
				}
				code := tc.status
				if tc.wantCalls == 2 {
					code = tc.publicCode
				}
				if (err == nil) != (code == http.StatusOK) {
					t.Errorf("error = %v, final HTTP status %d", err, code)
				}
				if code == http.StatusNotFound && !IsNotFound(err) {
					t.Errorf("want IsNotFound, got %v", err)
				}
				if code == http.StatusForbidden && !IsForbidden(err) {
					t.Errorf("want IsForbidden, got %v", err)
				}
				wantHeaders := []string{tc.token}
				if tc.wantCalls == 2 {
					wantHeaders = append(wantHeaders, "")
				}
				if !slices.Equal(headers, wantHeaders) {
					t.Errorf("headers = %q, want %q", headers, wantHeaders)
				}
				if client.token != tc.token {
					t.Error("fallback changed the client's token")
				}
			})
		}
	}
}
