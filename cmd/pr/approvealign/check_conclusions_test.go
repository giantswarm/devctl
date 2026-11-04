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

type checkConclusionsTransport func(*http.Request) (*http.Response, error)

func (f checkConclusionsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRCheckConclusions(t *testing.T) {
	for _, conclusion := range []string{"success", "neutral", "skipped", "failure", "cancelled", "timed_out", "action_required", "stale"} {
		t.Run(conclusion, func(t *testing.T) {
			accepted := conclusion == "success" || conclusion == "neutral" || conclusion == "skipped"
			reviewReads := 0
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: checkConclusionsTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected mutation: %s", req.URL.Path)
				}
				var body string
				switch req.URL.Path {
				case "/repos/fixture/repo/pulls/1":
					body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"}}`
				case "/repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"pending","total_count":0,"statuses":[]}`
				case "/repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":1,"check_runs":[{"name":"fixture-check","status":"completed","conclusion":"` + conclusion + `"}]}`
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
			ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
			(&runner{flag: &flag{DryRun: true}}).processPR(context.Background(), client, ps)
			want, wantReads := "Failed checks", 0
			if accepted {
				want, wantReads = "Would approve", 1
			}
			if ps.GetStatus() != want || reviewReads != wantReads {
				t.Errorf("conclusion %q: status %q, reviews read %d; want %q, %d", conclusion, ps.GetStatus(), reviewReads, want, wantReads)
			}
		})
	}
}
