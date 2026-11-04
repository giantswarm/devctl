package approvealign

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

type headTransport func(*http.Request) (*http.Response, error)

func (f headTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProcessPRCheckedHead(t *testing.T) {
	for _, tc := range []struct {
		name      string
		approved  bool
		behind    bool
		changed   bool
		mergeRace bool
		want      string
	}{
		{name: "review identifies checked commit", want: "Merged (auto-merge)"},
		{name: "merge unchanged head", approved: true, want: "Merged"},
		{name: "merge endpoint sees a push", approved: true, mergeRace: true, want: "Failed to merge"},
		{name: "refresh sees a push before merge", approved: true, changed: true, want: "Head changed; run again"},
		{name: "update approved branch", approved: true, behind: true},
		{name: "update newly approved branch", behind: true},
		{name: "refresh sees a push before update", approved: true, behind: true, changed: true, want: "Head changed; run again"},
		{name: "push after approval", behind: true, changed: true, want: "Head changed; run again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, reviews, merges, updates := 0, 0, 0, 0
			client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: headTransport(func(req *http.Request) (*http.Response, error) {
				code := http.StatusOK
				var body string
				write := map[string]string{}
				if req.Body != nil {
					if err := json.NewDecoder(req.Body).Decode(&write); err != nil {
						t.Fatal(err)
					}
					_ = req.Body.Close()
				}
				switch req.Method + " " + req.URL.Path {
				case "GET /repos/fixture/repo/pulls/1":
					reads++
					sha, state := "checked-head", "clean"
					if tc.changed && reads > 1 {
						sha = "unchecked-head"
					}
					if tc.behind && reads > 1 {
						state = "behind"
					}
					body = `{"title":"` + alignTitle + `","head":{"sha":"` + sha + `"},"base":{"ref":"main"},"mergeable_state":"` + state + `"}`
					if !tc.changed && ((!tc.approved && !tc.behind && reads > 1) || (tc.behind && reads > 2)) {
						body = `{"merged":true}`
					}
				case "GET /repos/fixture/repo/commits/checked-head/status":
					body = `{"state":"pending","total_count":0,"statuses":[]}`
				case "GET /repos/fixture/repo/commits/checked-head/check-runs":
					body = `{"total_count":0,"check_runs":[]}`
				case "GET /repos/fixture/repo/pulls/1/reviews":
					body = `[]`
					if tc.approved {
						body = `[{"state":"APPROVED","user":{"login":"reviewer"},"submitted_at":"2026-01-01T00:00:00Z"}]`
					}
				case "POST /repos/fixture/repo/pulls/1/reviews":
					reviews++
					if write["commit_id"] != "checked-head" || write["event"] != "APPROVE" {
						t.Errorf("review is not bound to the checked commit: %v", write)
					}
					code, body = http.StatusCreated, `{"state":"APPROVED","commit_id":"checked-head"}`
				case "PUT /repos/fixture/repo/pulls/1/merge":
					merges++
					if write["sha"] != "checked-head" || write["merge_method"] != "squash" {
						t.Errorf("merge is not bound to the checked commit: %v", write)
					}
					body = `{"merged":true}`
					if tc.mergeRace && write["sha"] == "checked-head" {
						code, body = http.StatusConflict, `{"message":"head changed"}`
					}
				case "PUT /repos/fixture/repo/pulls/1/update-branch":
					updates++
					if write["expected_head_sha"] != "checked-head" {
						t.Errorf("branch update is not bound to the checked commit: %v", write)
					}
					code, body = http.StatusAccepted, `{"message":"Updating pull request branch"}`
				default:
					t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}))
			if err != nil {
				t.Fatal(err)
			}
			ps := &pr.PRStatus{Owner: "fixture", Repo: "repo", Number: 1}
			(&runner{flag: &flag{Team: "fixture"}}).processPR(context.Background(), client, ps)
			if tc.want != "" && ps.GetStatus() != tc.want {
				t.Errorf("status %q, want %q", ps.GetStatus(), tc.want)
			}
			wantReviews, wantMerges, wantUpdates := 0, 0, 0
			if !tc.approved {
				wantReviews = 1
			}
			if !tc.changed {
				if tc.behind {
					wantUpdates = 1
				} else if tc.approved {
					wantMerges = 1
				}
			}
			if reviews != wantReviews || merges != wantMerges || updates != wantUpdates {
				t.Errorf("reviews/merges/updates = %d/%d/%d, want %d/%d/%d", reviews, merges, updates, wantReviews, wantMerges, wantUpdates)
			}
		})
	}
}
