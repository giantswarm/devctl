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

type mixedChecksTransport func(*http.Request) (*http.Response, error)

func (f mixedChecksTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRMixedChecks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     string
		conclusion string
		want       string
	}{
		{name: "failed Actions check", status: "completed", conclusion: "failure", want: "Failed checks"},
		{name: "pending Actions check", status: "in_progress", want: "Already merged"},
		{name: "successful Actions check", status: "completed", conclusion: "success", want: "Would approve"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			reviewReads := 0
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: mixedChecksTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected mutation: %s %s", req.Method, req.URL.Path)
				}
				var body string
				switch req.URL.Path {
				case "/repos/fixture/repo/pulls/1":
					reads++
					body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"},"merged":false}`
					if reads > 1 {
						// End the pending case after one poll, without live checks.
						body = `{"merged":true}`
					}
				case "/repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"success","total_count":1,"statuses":[{"context":"CircleCI","state":"success"}]}`
				case "/repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":1,"check_runs":[{"name":"pre-commit","status":"` + tc.status + `","conclusion":"` + tc.conclusion + `"}]}`
				case "/repos/fixture/repo/pulls/1/reviews":
					reviewReads++
					body = `[]`
				default:
					t.Fatalf("unexpected request: %s", req.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			r := &runner{flag: &flag{DryRun: true}}
			ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
			r.processPR(context.Background(), client, ps)
			if got := ps.GetStatus(); got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
			if tc.conclusion != "success" && reviewReads != 0 {
				t.Errorf("read reviews %d times before checks passed", reviewReads)
			}
		})
	}
}
