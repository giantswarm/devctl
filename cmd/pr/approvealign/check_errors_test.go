package approvealign

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type checkErrorsTransport func(*http.Request) (*http.Response, error)

func (f checkErrorsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRCheckReadErrors(t *testing.T) {
	for _, endpoint := range []string{"status", "check-runs"} {
		for _, failure := range []string{"403", "503", "transport", "invalid JSON", "none"} {
			t.Run(endpoint+"/"+failure, func(t *testing.T) {
				reviewReads := 0
				client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: checkErrorsTransport(func(req *http.Request) (*http.Response, error) {
					if req.Method != http.MethodGet {
						t.Fatalf("unexpected mutation: %s", req.URL.Path)
					}
					code := http.StatusOK
					var body string
					switch req.URL.Path {
					case "/repos/fixture/repo/pulls/1":
						body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"}}`
					case "/repos/fixture/repo/commits/checked-head/status":
						body = `{"state":"pending","total_count":0,"statuses":[]}`
					case "/repos/fixture/repo/commits/checked-head/check-runs":
						body = `{"total_count":0,"check_runs":[]}`
					case "/repos/fixture/repo/pulls/1/reviews":
						reviewReads++
						body = `[]`
					default:
						t.Fatalf("unexpected request: %s", req.URL.Path)
					}
					if strings.HasSuffix(req.URL.Path, "/commits/checked-head/"+endpoint) {
						switch failure {
						case "403":
							code, body = http.StatusForbidden, `{"message":"fixture forbidden"}`
						case "503":
							code, body = http.StatusServiceUnavailable, `{"message":"fixture unavailable"}`
						case "transport":
							return nil, errors.New("fixture transport failure")
						case "invalid JSON":
							body = `{`
						}
					}
					return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				})}))
				if err != nil {
					t.Fatal(err)
				}
				ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
				(&runner{flag: &flag{DryRun: true}}).processPR(context.Background(), client, ps)
				if failure == "none" {
					if ps.GetStatus() != "Would approve" || reviewReads != 1 {
						t.Fatalf("valid no-checks response: status %q, review reads %d", ps.GetStatus(), reviewReads)
					}
				} else if !strings.HasPrefix(ps.GetStatus(), "Failed to get") || reviewReads != 0 {
					t.Errorf("failed check read: status %q, review reads %d; want failure before reviews", ps.GetStatus(), reviewReads)
				}
			})
		}
	}
}
