package circleciclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListWorkflowJobsPages(t *testing.T) {
	testCases := []struct {
		name       string
		secondPage string
		status     int
		want       []Job
		wantError  func(error) bool
	}{
		{
			name:       "case 0: every page counts including a blocked downstream job",
			secondPage: `{"items":[{"id":"j2","name":"test","status":"blocked"}],"next_page_token":null}`,
			want: []Job{
				{ID: "j1", Name: "build", Status: "success"},
				{ID: "j2", Name: "test", Status: "blocked"},
			},
		},
		{
			name:       "case 1: an unreadable later page returns no partial jobs",
			secondPage: `{"message":"Not Found"}`,
			status:     http.StatusNotFound,
			wantError:  IsNotFound,
		},
		{
			name:       "case 2: a repeated page token returns no partial jobs",
			secondPage: `{"items":[],"next_page_token":"next+/="}`,
			wantError:  func(err error) bool { return err != nil },
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/workflow/w1/job" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				token := r.URL.Query().Get("page-token")
				queries = append(queries, token)
				w.Header().Set("Content-Type", "application/json")
				if token == "" {
					_, _ = w.Write([]byte(`{"items":[{"id":"j1","name":"build","status":"success"}],"next_page_token":"next+/="}`))
					return
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.secondPage))
			}))
			t.Cleanup(srv.Close)
			client, err := New(Config{Token: "cci_test", BaseURL: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			jobs, err := client.ListWorkflowJobs(context.Background(), "w1")
			if tc.wantError != nil {
				if !tc.wantError(err) {
					t.Fatalf("want matching error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tc.want, jobs); diff != "" {
				t.Errorf("jobs (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]string{"", "next+/="}, queries); diff != "" {
				t.Errorf("page tokens (-want +got):\n%s", diff)
			}
		})
	}
}
