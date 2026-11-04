package approvealign

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type reviewHistoryTransport func(*http.Request) (*http.Response, error)

func (f reviewHistoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRReviewHistory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		states   []string
		other    bool
		paged    bool
		failPage bool
		want     string
	}{
		{name: "approval withdrawn", states: []string{"APPROVED", "CHANGES_REQUESTED"}, want: "Would approve"},
		{name: "comment preserves approval", states: []string{"APPROVED", "COMMENTED"}, want: "Already approved"},
		{name: "pending preserves approval", states: []string{"APPROVED", "PENDING"}, want: "Already approved"},
		{name: "renewed approval", states: []string{"CHANGES_REQUESTED", "APPROVED"}, want: "Already approved"},
		{name: "dismissed approval", states: []string{"DISMISSED"}, want: "Would approve"},
		{name: "another reviewer still approves", states: []string{"APPROVED", "CHANGES_REQUESTED"}, other: true, want: "Already approved"},
		{name: "approval on later page", states: []string{"COMMENTED", "APPROVED"}, paged: true, want: "Already approved"},
		{name: "withdrawal on later page", states: []string{"APPROVED", "CHANGES_REQUESTED"}, paged: true, want: "Would approve"},
		{name: "later page fails", states: []string{"APPROVED", "CHANGES_REQUESTED"}, paged: true, failPage: true, want: "Failed to get reviews: HTTP 503 fixture unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pages := 0
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: reviewHistoryTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet {
					t.Fatalf("unexpected mutation: %s", req.URL.Path)
				}
				code := http.StatusOK
				header := make(http.Header)
				var body string
				switch req.URL.Path {
				case "/repos/fixture/repo/pulls/1":
					body = `{"title":"` + alignTitle + `","head":{"sha":"checked-head"},"mergeable_state":"clean"}`
				case "/repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"pending","total_count":0,"statuses":[]}`
				case "/repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":0,"check_runs":[]}`
				case "/repos/fixture/repo/pulls/1/reviews":
					pages++
					states := tc.states
					if tc.paged {
						perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
						if perPage == 0 {
							perPage = 30
						}
						if req.URL.Query().Get("page") == "2" {
							states = tc.states[1:]
						} else {
							states = []string{tc.states[0]}
							for len(states) < perPage {
								states = append(states, "COMMENTED")
							}
							header.Set("Link", "<https://api.github.com"+req.URL.Path+"?page=2&per_page="+strconv.Itoa(perPage)+">; rel=\"next\"")
						}
					}
					reviews := []*github.PullRequestReview{}
					for i, state := range states {
						login := "reviewer"
						if pages == 1 && i == 0 {
							login = "Reviewer"
						}
						reviews = append(reviews, &github.PullRequestReview{State: new(state), User: &github.User{Login: new(login)}})
					}
					if tc.other {
						reviews = append(reviews, &github.PullRequestReview{State: new("APPROVED"), User: &github.User{Login: new("other-reviewer")}})
					}
					raw, err := json.Marshal(reviews)
					if err != nil {
						t.Fatal(err)
					}
					body = string(raw)
					if tc.failPage && req.URL.Query().Get("page") == "2" {
						code, body = http.StatusServiceUnavailable, `{"message":"fixture unavailable"}`
					}
				default:
					t.Fatalf("unexpected request: %s", req.URL.Path)
				}
				return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
			(&runner{flag: &flag{DryRun: true}}).processPR(context.Background(), client, ps)
			if ps.GetStatus() != tc.want {
				t.Errorf("status %q, want %q", ps.GetStatus(), tc.want)
			}
			if tc.paged && pages != 2 {
				t.Errorf("review pages read = %d, want 2", pages)
			}
		})
	}
}
