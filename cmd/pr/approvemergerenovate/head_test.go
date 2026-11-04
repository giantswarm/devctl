package approvemergerenovate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

func TestProcessPRPinsMutationsToCheckedHead(t *testing.T) {
	for _, moveHead := range []bool{false, true} {
		t.Run(fmt.Sprintf("head_moves=%t", moveHead), func(t *testing.T) {
			currentHead, reviewHead, mergeHead := "checked-head", "", ""
			merged := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case req.URL.Path == "/repos/one/api/pulls/42":
					fmt.Fprintf(w, `{"head":{"sha":%q}}`, currentHead)
				case strings.HasSuffix(req.URL.Path, "/status"):
					fmt.Fprint(w, `{"state":"success","total_count":1}`)
				case strings.HasSuffix(req.URL.Path, "/check-runs"):
					fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
				case strings.HasSuffix(req.URL.Path, "/reviews") && req.Method == http.MethodGet:
					if moveHead {
						currentHead = "unchecked-head"
					}
					fmt.Fprint(w, `[]`)
				case strings.HasSuffix(req.URL.Path, "/reviews") && req.Method == http.MethodPost:
					var body struct {
						CommitID string `json:"commit_id"`
					}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					reviewHead = body.CommitID
					if reviewHead == "" {
						reviewHead = currentHead
					}
					fmt.Fprint(w, `{}`)
				case req.URL.Path == "/repos/one/api":
					fmt.Fprint(w, `{"allow_squash_merge":true}`)
				case strings.HasSuffix(req.URL.Path, "/merge"):
					var body struct {
						SHA string `json:"sha"`
					}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					mergeHead = body.SHA
					if body.SHA != "" && body.SHA != currentHead {
						w.WriteHeader(http.StatusConflict)
						fmt.Fprint(w, `{"message":"Head branch was modified"}`)
						return
					}
					merged = true
					fmt.Fprint(w, `{"merged":true}`)
				default:
					t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client, err := github.NewClient(github.WithHTTPClient(server.Client()), github.WithURLs(new(server.URL+"/"), nil))
			if err != nil {
				t.Fatal(err)
			}
			ps := &pr.PRStatus{Owner: "one", Repo: "api", Number: 42}
			(&runner{flag: &flag{}}).processPR(context.Background(), client, ps)
			if reviewHead != "checked-head" {
				t.Errorf("approved %q, want checked-head", reviewHead)
			}
			if mergeHead != "checked-head" {
				t.Errorf("merge guard = %q, want checked-head", mergeHead)
			}
			if merged == moveHead {
				t.Errorf("merged = %t with moving head = %t", merged, moveHead)
			}
			want := "Merged (squash)"
			if moveHead {
				want = "Merge failed"
			}
			if got := ps.GetStatus(); got != want {
				t.Errorf("status = %q, want %q", got, want)
			}
		})
	}
}
