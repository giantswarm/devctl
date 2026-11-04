package approvemergerenovate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

func TestProcessPRRequiresReadablePassingChecks(t *testing.T) {
	tests := []struct {
		name, state, conclusion          string
		statusCode, checksCode           int
		secondPage, pendingFirst, dryRun bool
		want                             string
		mutations                        int
	}{
		{name: "passing status cannot hide failed Actions", state: "success", conclusion: "failure", want: "Failed checks"},
		{name: "status API failure", statusCode: 500, want: "Failed to get checks"},
		{name: "checks API failure", checksCode: 403, want: "Failed to get checks"},
		{name: "failure on second check page", secondPage: true, want: "Failed checks"},
		{name: "action required", conclusion: "action_required", want: "Failed checks"},
		{name: "stale", conclusion: "stale", want: "Failed checks"},
		{name: "startup failure", conclusion: "startup_failure", want: "Failed checks"},
		{name: "cancelled", conclusion: "cancelled", want: "Failed checks"},
		{name: "timed out", conclusion: "timed_out", want: "Failed checks"},
		{name: "pending Actions then green status and checks", state: "success", conclusion: "success", pendingFirst: true, want: "Merged (squash)", mutations: 2},
		{name: "neutral is allowed", conclusion: "neutral", want: "Merged (squash)", mutations: 2},
		{name: "skipped is allowed", conclusion: "skipped", want: "Merged (squash)", mutations: 2},
		{name: "no checks exist", want: "Merged (squash)", mutations: 2},
		{name: "dry run green", state: "success", conclusion: "success", dryRun: true, want: "Would merge (squash)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutations, checkReads := 0, 0
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if req.Method != http.MethodGet {
					mutations++
				}
				switch {
				case req.URL.Path == "/repos/one/api/pulls/42" && req.Method == http.MethodGet:
					fmt.Fprint(w, `{"head":{"sha":"checked-head"}}`)
				case strings.HasSuffix(req.URL.Path, "/status"):
					if tt.statusCode != 0 {
						w.WriteHeader(tt.statusCode)
						fmt.Fprint(w, `{"message":"unavailable"}`)
						return
					}
					state, count := tt.state, 1
					if state == "" {
						state, count = "pending", 0
					}
					fmt.Fprintf(w, `{"state":%q,"total_count":%d}`, state, count)
				case strings.HasSuffix(req.URL.Path, "/check-runs"):
					checkReads++
					if tt.checksCode != 0 {
						w.WriteHeader(tt.checksCode)
						fmt.Fprint(w, `{"message":"unavailable"}`)
						return
					}
					if tt.secondPage {
						if req.URL.Query().Get("page") == "2" {
							fmt.Fprint(w, `{"total_count":2,"check_runs":[{"status":"completed","conclusion":"failure"}]}`)
						} else {
							w.Header().Set("Link", "<"+server.URL+req.URL.Path+"?page=2>; rel=\"next\"")
							fmt.Fprint(w, `{"total_count":2,"check_runs":[{"status":"completed","conclusion":"success"}]}`)
						}
					} else if tt.pendingFirst && checkReads == 1 {
						fmt.Fprint(w, `{"total_count":1,"check_runs":[{"status":"in_progress"}]}`)
					} else if tt.conclusion != "" {
						fmt.Fprintf(w, `{"total_count":1,"check_runs":[{"status":"completed","conclusion":%q}]}`, tt.conclusion)
					} else {
						fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
					}
				case strings.HasSuffix(req.URL.Path, "/reviews"):
					if req.Method == http.MethodGet {
						fmt.Fprint(w, `[]`)
					} else {
						fmt.Fprint(w, `{}`)
					}
				case req.URL.Path == "/repos/one/api":
					fmt.Fprint(w, `{"allow_squash_merge":true}`)
				case strings.HasSuffix(req.URL.Path, "/merge"):
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
			r := &runner{flag: &flag{DryRun: tt.dryRun}, stdout: io.Discard, stderr: io.Discard}
			ps := &pr.PRStatus{Owner: "one", Repo: "api", Number: 42}
			r.processPR(context.Background(), client, ps)
			if got := ps.GetStatus(); got != tt.want {
				t.Errorf("status = %q, want %q", got, tt.want)
			}
			if mutations != tt.mutations {
				t.Errorf("mutations = %d, want %d", mutations, tt.mutations)
			}
			if tt.pendingFirst && checkReads != 2 {
				t.Errorf("check reads = %d, want pending followed by green", checkReads)
			}
		})
	}
}
