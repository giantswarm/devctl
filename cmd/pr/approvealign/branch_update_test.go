package approvealign

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type branchUpdateTransport func(*http.Request) (*http.Response, error)

func (f branchUpdateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRAcceptedBranchUpdate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		approved  bool
		autoMerge bool
		merged    bool
		pending   bool
		code      int
		want      string
	}{
		{name: "approved update merges", approved: true, merged: true, code: 202, want: "Merged (auto-merge)"},
		{name: "new approval update merges", merged: true, code: 202, want: "Merged (auto-merge)"},
		{name: "approved update queues merge", approved: true, autoMerge: true, code: 202, want: "Updated, queued to merge"},
		{name: "new approval branch updated", code: 202, want: "Branch updated"},
		{name: "approved update still queued", approved: true, pending: true, code: 202, want: "Waiting for branch update"},
		{name: "new approval update still queued", pending: true, code: 202, want: "Waiting for branch update"},
		{name: "approved update forbidden", approved: true, code: 403, want: "Failed to update branch"},
		{name: "new approval update rejected", code: 422, want: "Failed to update branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, updates, approvals := 0, 0, 0
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: branchUpdateTransport(func(req *http.Request) (*http.Response, error) {
				code := http.StatusOK
				var body string
				if req.Body != nil {
					_ = req.Body.Close()
				}
				switch req.Method + " " + req.URL.Path {
				case "GET /repos/fixture/repo/pulls/1":
					reads++
					sha := "checked-head"
					if reads > 2 && !tc.pending {
						sha = "updated-head"
					}
					body = `{"title":"` + alignTitle + `","head":{"sha":"` + sha + `"},"mergeable_state":"behind"`
					if tc.autoMerge {
						body += `,"auto_merge":{}`
					}
					if tc.merged && reads > 2 {
						body += `,"merged":true`
					}
					body += `}`
				case "GET /repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"pending","total_count":0,"statuses":[]}`
				case "GET /repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":0,"check_runs":[]}`
				case "GET /repos/fixture/repo/pulls/1/reviews":
					body = `[]`
					if tc.approved {
						body = `[{"state":"APPROVED","user":{"login":"reviewer"}}]`
					}
				case "POST /repos/fixture/repo/pulls/1/reviews":
					approvals++
					code, body = http.StatusCreated, `{"state":"APPROVED"}`
				case "PUT /repos/fixture/repo/pulls/1/update-branch":
					updates++
					code, body = tc.code, `{"message":"Updating pull request branch"}`
				default:
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
			(&runner{flag: &flag{}}).processPR(context.Background(), client, ps)
			wantReads, wantApprovals := 2, 0
			if tc.code == http.StatusAccepted {
				wantReads = 3
			}
			if !tc.approved {
				wantApprovals = 1
			}
			if ps.GetStatus() != tc.want || updates != 1 || reads != wantReads || approvals != wantApprovals {
				t.Errorf("status %q, updates/reads/approvals = %d/%d/%d; want %q, 1/%d/%d", ps.GetStatus(), updates, reads, approvals, tc.want, wantReads, wantApprovals)
			}
		})
	}
}
