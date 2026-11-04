package approvemergerenovate

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

type headTransport func(*http.Request) (*http.Response, error)

func (f headTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestProcessPRBindsWritesToCheckedHead(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(map[bool]string{false: "approve", true: "merge"}[approved], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reviews, merges := 0, 0
				transport := headTransport(func(req *http.Request) (*http.Response, error) {
					var body any
					code := http.StatusOK
					switch req.Method + " " + req.URL.Path {
					case "GET /repos/o/r/pulls/7":
						body = map[string]any{"head": map[string]any{"sha": "tested"}, "mergeable_state": "clean"}
					case "GET /repos/o/r/commits/tested/status":
						body = map[string]any{"state": "success", "total_count": 0}
					case "GET /repos/o/r/commits/tested/check-runs":
						body = map[string]any{"check_runs": []any{map[string]any{"name": "test", "status": "completed", "conclusion": "success"}}}
					case "GET /repos/o/r/pulls/7/reviews":
						body = []any{}
						if approved {
							body = []any{map[string]any{"state": "APPROVED"}}
						}
					case "POST /repos/o/r/pulls/7/reviews":
						reviews++
						var review map[string]any
						if err := json.NewDecoder(req.Body).Decode(&review); err != nil {
							t.Fatal(err)
						}
						if review["commit_id"] != "tested" {
							t.Errorf("approval targets %v; without tested commit_id GitHub approves a concurrently pushed head", review)
						}
						body = map[string]any{"state": "APPROVED"}
					case "GET /repos/o/r":
						body = map[string]any{"allow_squash_merge": true}
					case "PUT /repos/o/r/pulls/7/merge":
						merges++
						var merge map[string]any
						if err := json.NewDecoder(req.Body).Decode(&merge); err != nil {
							t.Fatal(err)
						}
						// A push arrived after the check read. GitHub can refuse it only
						// when the request carries the checked SHA.
						if merge["sha"] != "tested" {
							t.Errorf("merge targets %v; the unchecked replacement head can land", merge)
						}
						code = http.StatusConflict
						body = map[string]any{"message": "Head branch was modified"}
					default:
						t.Fatalf("unexpected request %s %s", req.Method, req.URL)
					}
					raw, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
				})
				client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: transport}))
				if err != nil {
					t.Fatal(err)
				}
				r := &runner{flag: &flag{}}
				ps := &pr.PRStatus{Owner: "o", Repo: "r", Number: 7}
				r.processPR(context.Background(), client, ps)
				if !approved && reviews != 1 {
					t.Errorf("approvals=%d, want one", reviews)
				}
				if approved && merges != 1 {
					t.Errorf("merge requests=%d, want one", merges)
				}
			})
		})
	}
}
