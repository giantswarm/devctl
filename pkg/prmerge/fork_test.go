package prmerge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// Test_Merge_forkClassification exercises the real team-file policy and merge
// request together. A missing or unreadable declaration must not silently
// squash the commits of a fork line, even when GitHub permits squash merges.
func Test_Merge_forkClassification(t *testing.T) {
	tests := []struct {
		name       string
		owner      string
		entry      string
		fork       bool
		method     githubclient.MergeMethod
		wantMethod string
		wantReason string
	}{
		{name: "case 0: declared fork line defaults to rebase", entry: "- name: line\n  gen:\n    flavours: [fork]\n", fork: true, wantMethod: "rebase"},
		{name: "case 1: undeclared fork is refused", fork: true, wantReason: "GitHub identifies giantswarm/line as a fork"},
		{name: "case 2: fork without gen is refused", entry: "- name: line\n", fork: true, wantReason: "gen.flavours"},
		{name: "case 3: fork with other flavours is refused", entry: "- name: line\n  gen:\n    flavours: [app]\n", fork: true, wantReason: "gen.flavours"},
		{name: "case 4: unreadable flavours are refused even on a detached fork", entry: "- name: line\n  gen:\n    flavours: fork\n", wantReason: "could not be read"},
		{name: "case 5: fork outside giantswarm is refused", owner: "other", fork: true, wantReason: "GitHub identifies other/line as a fork"},
		{name: "case 6: explicit rebase on an undeclared fork", fork: true, method: githubclient.MergeRebase, wantMethod: "rebase"},
		{name: "case 7: explicit squash on an undeclared fork", fork: true, method: githubclient.MergeSquash, wantMethod: "squash"},
		{name: "case 8: explicit rebase with unreadable flavours", entry: "- name: line\n  gen:\n    flavours: fork\n", method: githubclient.MergeRebase, wantMethod: "rebase"},
		{name: "case 9: ordinary undeclared repository keeps squash", wantMethod: "squash"},
		{name: "case 10: declared detached fork line defaults to rebase", entry: "- name: line\n  gen:\n    flavours: [fork]\n", wantMethod: "rebase"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner := tc.owner
			if owner == "" {
				owner = "giantswarm"
			}
			slug := owner + "/line"
			pr := pull(map[string]any{
				"head": map[string]any{"sha": "abc123", "ref": "feature", "repo": map[string]any{"full_name": slug}},
				"base": map[string]any{"ref": "giantswarm", "repo": map[string]any{"full_name": slug}},
			})
			repository := settings(true, true, true)
			repository["full_name"], repository["fork"] = slug, tc.fork
			r := sequence.Routes{}
			for key, responses := range routes(pr, sequence.Routes{"GET /repos/o/r": {{Body: repository}}}) {
				key = strings.ReplaceAll(key, "/repos/o/r", "/repos/"+slug)
				key = strings.ReplaceAll(key, "/compare/main...", "/compare/giantswarm...")
				r[key] = responses
			}
			entry := tc.entry
			if entry == "" {
				entry = "- name: another-repository\n"
			}
			r["GET /repos/giantswarm/github/contents/repositories"] = []sequence.Response{{Body: []any{
				map[string]any{"type": "file", "name": "team-a.yaml", "path": "repositories/team-a.yaml"},
			}}}
			r["GET /repos/giantswarm/github/contents/repositories/team-a.yaml"] = []sequence.Response{{Body: map[string]any{
				"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(entry)),
			}}}
			server, err := githubmock.Start(r)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.Close)
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			upstream := httputil.NewSingleHostReverseProxy(target)
			var body struct {
				Method string `json:"merge_method"`
				SHA    string `json:"sha"`
			}
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge") {
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode merge request: %v", err)
					}
					r.Body, r.ContentLength = http.NoBody, 0
				}
				upstream.ServeHTTP(w, r)
			}))
			t.Cleanup(proxy.Close)
			h := newHarnessAt(t, server, proxy.URL, func(c *Config) {
				c.Policy = nil // Use the production TeamFilePolicy.
				c.Method = tc.method
				c.UpdateBranch = true
			})
			result, err := h.merger.Merge(context.Background(), owner, "line", 42)
			if tc.wantReason != "" {
				var exitErr *agentcli.ExitError
				if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitNotApplicable || exitErr.Verdict != agentcli.VerdictNotApplicable || !strings.Contains(exitErr.Reason, tc.wantReason) {
					t.Fatalf("want exit 3 naming %q, got %v", tc.wantReason, err)
				}
				if !strings.Contains(exitErr.Reason, "--rebase") || !strings.Contains(exitErr.Reason, "--squash") {
					t.Errorf("refusal must name explicit method choices: %s", exitErr.Reason)
				}
				if result.MergeCommitSHA != "" || result.BranchDeleted || result.Enqueued {
					t.Errorf("refused merge reported a write: %+v", result)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if result.Method != tc.wantMethod || result.MergeCommitSHA != "m1" {
				t.Errorf("want a successful %s merge, got %+v", tc.wantMethod, result)
			}

			merges := 0
			for _, request := range h.server.Requests() {
				if tc.wantReason != "" {
					if request.Method != http.MethodGet || strings.Contains(request.Path, "/check-runs") || strings.Contains(request.Path, "/compare/") {
						t.Errorf("refused merge must not wait, update, enqueue, merge or delete: %s %s", request.Method, request.Path)
					}
					continue
				}
				if request.Method == http.MethodPut && strings.HasSuffix(request.Path, "/merge") {
					merges++
					if body.Method != tc.wantMethod || body.SHA != "abc123" {
						t.Errorf("want merge method %s and judged head abc123, got %+v", tc.wantMethod, body)
					}
				}
			}
			if tc.wantReason == "" && merges != 1 {
				t.Errorf("want exactly one merge, got %d", merges)
			}
		})
	}
}
