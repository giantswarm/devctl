package approvealign

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type checkPaginationTransport func(*http.Request) (*http.Response, error)

func (f checkPaginationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRCheckPagination(t *testing.T) {
	for _, tc := range []struct {
		name       string
		source     string
		lastState  string
		failPage   bool
		diagnostic bool
		want       string
	}{
		{name: "later failed run", source: "check-runs", lastState: "failure", want: "Failed checks"},
		{name: "later pending run", source: "check-runs", lastState: "pending", want: "Already merged"},
		{name: "all runs green", source: "check-runs", lastState: "success", want: "Would approve"},
		{name: "later runs unavailable", source: "check-runs", failPage: true, want: "Failed to get checks: reading check runs"},
		{name: "later required status exists", source: "status", diagnostic: true, want: "Already approved, blocked"},
		{name: "later statuses unavailable", source: "status", failPage: true, want: "Failed to get checks: reading commit statuses"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := 0
			prReads := 0
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: checkPaginationTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected mutation: %s", req.URL.Path)
				}
				code := http.StatusOK
				header := make(http.Header)
				body := ""
				page := req.URL.Query().Get("page")
				perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
				if perPage == 0 {
					perPage = 30
				}
				switch req.URL.Path {
				case "/repos/fixture/repo/pulls/1":
					prReads++
					body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"},"base":{"ref":"main"},"mergeable_state":"blocked"}`
					if tc.lastState == "pending" && prReads > 1 {
						body = `{"merged":true}`
					}
				case "/repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"pending","total_count":0,"statuses":[]}`
					if tc.source == "status" {
						var statuses []*github.RepoStatus
						if page == "2" {
							statuses = append(statuses, &github.RepoStatus{Context: new("late-required"), State: new("success")})
						} else {
							for i := 0; i < perPage; i++ {
								statuses = append(statuses, &github.RepoStatus{Context: new(fmt.Sprintf("status-%d", i)), State: new("success")})
							}
						}
						raw, err := json.Marshal(&github.CombinedStatus{State: new("success"), TotalCount: new(perPage + 1), Statuses: statuses})
						if err != nil {
							t.Fatal(err)
						}
						body = string(raw)
					}
				case "/repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":0,"check_runs":[]}`
					if tc.source == "check-runs" {
						var runs []*github.CheckRun
						if page == "2" {
							status, conclusion := "completed", tc.lastState
							if conclusion == "pending" {
								status, conclusion = "in_progress", ""
							}
							runs = append(runs, &github.CheckRun{Name: new("late-check"), Status: new(status), Conclusion: new(conclusion)})
						} else {
							for i := 0; i < perPage; i++ {
								runs = append(runs, &github.CheckRun{Name: new(fmt.Sprintf("check-%d", i)), Status: new("completed"), Conclusion: new("success")})
							}
						}
						raw, err := json.Marshal(&github.ListCheckRunsResults{Total: new(perPage + 1), CheckRuns: runs})
						if err != nil {
							t.Fatal(err)
						}
						body = string(raw)
					}
				case "/repos/fixture/repo/pulls/1/reviews":
					body = `[]`
					if tc.diagnostic {
						body = `[{"user":{"login":"reviewer"},"state":"APPROVED","submitted_at":"2026-01-01T00:00:00Z"}]`
					}
				case "/repos/fixture/repo/rules/branches/main":
					body = `[{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"late-required"}]}}]`
				default:
					t.Fatalf("unexpected request: %s", req.URL.Path)
				}
				if strings.HasSuffix(req.URL.Path, "/commits/checked-head/"+tc.source) {
					pages++
					if page != "2" {
						header.Set("Link", "<https://api.github.com"+req.URL.Path+"?page=2&per_page="+strconv.Itoa(perPage)+">; rel=\"next\"")
					} else if tc.failPage {
						code, body = http.StatusServiceUnavailable, `{"message":"fixture later page unavailable"}`
					}
				}
				return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
			(&runner{flag: &flag{DryRun: true, Team: "fixture"}}).processPR(context.Background(), client, ps)
			if !strings.HasPrefix(ps.GetStatus(), tc.want) || pages != 2 {
				t.Errorf("status %q, pages read %d; want %q, 2", ps.GetStatus(), pages, tc.want)
			}
		})
	}
}
