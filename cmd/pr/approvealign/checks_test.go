package approvealign

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type checksTransport func(*http.Request) (*http.Response, error)

func (f checksTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// A successful status from one CI does not stand in for all check runs.
// An unreadable or incomplete check listing must never authorize a write.
func TestProcessPRChecks(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		state                 string
		statusCode, runsCode  int
		conclusion, runStatus string
		secondPage            bool
		want                  string
	}{
		{name: "green status red Actions", state: "success", conclusion: "failure", runStatus: "completed", want: "Failed checks"},
		{name: "green status pending Actions", state: "success", runStatus: "in_progress", want: "Timeout"},
		{name: "status read unavailable", state: "success", statusCode: 503, conclusion: "success", runStatus: "completed", want: "Failed to get checks"},
		{name: "check read forbidden", state: "success", runsCode: 403, want: "Failed to get checks"},
		{name: "red check on second page", state: "pending", conclusion: "success", runStatus: "completed", secondPage: true, want: "Failed checks"},
		{name: "action required", state: "pending", conclusion: "action_required", runStatus: "completed", want: "Failed checks"},
		{name: "startup failure", state: "pending", conclusion: "startup_failure", runStatus: "completed", want: "Failed checks"},
		{name: "green checks", state: "success", conclusion: "success", runStatus: "completed", want: "Already approved"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				decisions := 0
				transport := checksTransport(func(req *http.Request) (*http.Response, error) {
					code := http.StatusOK
					headers := http.Header{"Content-Type": {"application/json"}}
					var body any
					switch req.URL.Path {
					case "/repos/o/r/pulls/7":
						body = map[string]any{"title": alignTitle, "head": map[string]any{"sha": "abc"}, "mergeable_state": "clean"}
					case "/repos/o/r/commits/abc/status":
						if tc.statusCode != 0 {
							code = tc.statusCode
						}
						body = map[string]any{"state": tc.state, "total_count": 0, "statuses": []any{}}
						if tc.state == "success" {
							body = map[string]any{"state": "success", "total_count": 1, "statuses": []any{map[string]any{"context": "ci/circleci: build", "state": "success"}}}
						}
					case "/repos/o/r/commits/abc/check-runs":
						if tc.runsCode != 0 {
							code = tc.runsCode
						}
						conclusion := tc.conclusion
						if tc.secondPage {
							if req.URL.Query().Get("page") == "2" {
								conclusion = "failure"
							} else {
								headers.Set("Link", `<https://fixture.invalid/repos/o/r/commits/abc/check-runs?page=2>; rel="next"`)
							}
						}
						body = map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"name": "test", "status": tc.runStatus, "conclusion": conclusion}}}
					case "/repos/o/r/pulls/7/reviews":
						decisions++
						body = []any{map[string]any{"state": "APPROVED", "user": map[string]any{"login": "reviewer"}}}
					case "/repos/o/r":
						body = map[string]any{"allow_squash_merge": true}
					default:
						t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
					}
					raw, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
				})
				client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}), github.WithURLs(new("https://fixture.invalid/"), nil))
				if err != nil {
					t.Fatal(err)
				}
				r := &runner{flag: &flag{DryRun: true}}
				ps := &pr.PRStatus{Owner: "o", Repo: "r", Number: 7}
				r.processPR(context.Background(), client, ps)
				if !strings.HasPrefix(ps.GetStatus(), tc.want) {
					t.Errorf("status = %q, want prefix %q", ps.GetStatus(), tc.want)
				}
				if tc.want != "Already approved" && decisions != 0 {
					t.Errorf("checked reviews %d times despite checks not being green", decisions)
				}
			})
		})
	}
}
