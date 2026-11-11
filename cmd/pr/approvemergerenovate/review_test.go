package approvemergerenovate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

func TestProcessPRNeedsCallersEffectiveApproval(t *testing.T) {
	tests := []struct {
		name, first, second      string
		approvals                int
		userFailure, pageFailure bool
		want                     string
	}{
		{name: "another reviewer", first: `[{"state":"APPROVED","user":{"login":"bob"}}]`, approvals: 1},
		{name: "caller approved", first: `[{"state":"APPROVED","user":{"login":"ALICE"}}]`},
		{name: "caller changed review later", first: `[{"state":"APPROVED","user":{"login":"alice"}}]`, second: `[{"state":"CHANGES_REQUESTED","user":{"login":"alice"}}]`, approvals: 1},
		{name: "caller approval dismissed", first: `[{"state":"DISMISSED","user":{"login":"alice"}}]`, approvals: 1},
		{name: "comment does not revoke approval", first: `[{"state":"APPROVED","user":{"login":"alice"}},{"state":"COMMENTED","user":{"login":"alice"}}]`},
		{name: "approval on later page", first: `[{"state":"CHANGES_REQUESTED","user":{"login":"alice"}}]`, second: `[{"state":"APPROVED","user":{"login":"alice"}}]`},
		{name: "caller identity unreadable", first: `[{"state":"APPROVED","user":{"login":"bob"}}]`, userFailure: true, want: "Failed to get reviewer"},
		{name: "later reviews unreadable", first: `[{"state":"APPROVED","user":{"login":"alice"}}]`, pageFailure: true, want: "Failed to get reviews"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			approvals, merges := 0, 0
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case req.URL.Path == "/user":
					if tt.userFailure {
						w.WriteHeader(401)
						fmt.Fprint(w, `{"message":"unauthorized"}`)
						return
					}
					fmt.Fprint(w, `{"login":"alice"}`)
				case req.URL.Path == "/repos/one/api/pulls/42":
					fmt.Fprint(w, `{"head":{"sha":"head"}}`)
				case strings.HasSuffix(req.URL.Path, "/status"):
					fmt.Fprint(w, `{"state":"success","total_count":1}`)
				case strings.HasSuffix(req.URL.Path, "/check-runs"):
					fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
				case strings.HasSuffix(req.URL.Path, "/reviews") && req.Method == http.MethodGet:
					if req.URL.Query().Get("page") == "2" {
						if tt.pageFailure {
							w.WriteHeader(500)
							fmt.Fprint(w, `{"message":"unavailable"}`)
							return
						}
						fmt.Fprint(w, tt.second)
					} else {
						if tt.second != "" || tt.pageFailure {
							w.Header().Set("Link", "<"+server.URL+req.URL.Path+"?page=2>; rel=\"next\"")
						}
						fmt.Fprint(w, tt.first)
					}
				case strings.HasSuffix(req.URL.Path, "/reviews") && req.Method == http.MethodPost:
					approvals++
					fmt.Fprint(w, `{}`)
				case req.URL.Path == "/repos/one/api":
					fmt.Fprint(w, `{"allow_squash_merge":true}`)
				case strings.HasSuffix(req.URL.Path, "/merge"):
					merges++
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
			want := tt.want
			wantMerges := 0
			if want == "" {
				want = testMergedSquashStatus
				wantMerges = 1
			}
			if got := ps.GetStatus(); got != want {
				t.Errorf("status = %q, want %q", got, want)
			}
			if approvals != tt.approvals || merges != wantMerges {
				t.Errorf("approvals/merges = %d/%d, want %d/%d", approvals, merges, tt.approvals, wantMerges)
			}
		})
	}
}
