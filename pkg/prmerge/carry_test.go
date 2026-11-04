package prmerge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
)

func carriedCommit(sha string, id int) map[string]any {
	return map[string]any{
		"sha": sha, "author": map[string]any{"id": id, "login": "alice"},
		"commit": map[string]any{"author": map[string]any{"name": "Alice", "email": "alice@example.invalid"}},
	}
}

func carryRoutes() sequence.Routes {
	return routes(pull(map[string]any{"base": map[string]any{"sha": "base123", "ref": "main", "repo": map[string]any{"full_name": "o/r"}}}), sequence.Routes{
		"GET /repos/o/r/pulls/12":                 {{Body: map[string]any{"number": 12, "user": map[string]any{"id": 7, "login": "alice"}}}},
		"GET /repos/o/r/compare/base123...abc123": {{Body: map[string]any{"total_commits": 2, "commits": []any{carriedCommit("patch1", 7), carriedCommit("abc123", 7)}}}},
	})
}

func Test_Merge_carryOf(t *testing.T) {
	tests := []struct {
		name       string
		change     func(sequence.Routes)
		carryOf    int
		wantCode   int
		wantReason string
		waited     bool
	}{
		{name: "case 0: every commit retains the colleague's author", carryOf: 12, waited: true},
		{name: "case 1: the carrier is not the original", carryOf: 42, wantCode: 5, wantReason: "must differ"},
		{name: "case 2: a later page contains a carrier commit", carryOf: 12, wantCode: 5, wantReason: "not verifiably authored by alice", change: func(r sequence.Routes) {
			r["GET /repos/o/r/compare/base123...abc123"] = []sequence.Response{{
				Headers: map[string]string{"Link": `<https://api.github.test/compare?page=2>; rel="next"`},
				Body:    map[string]any{"total_commits": 2, "commits": []any{carriedCommit("patch1", 7)}},
			}}
			r["GET /repos/o/r/compare/base123...abc123?page=2"] = []sequence.Response{{Body: map[string]any{"total_commits": 2, "commits": []any{carriedCommit("abc123", 8)}}}}
		}},
		{name: "case 3: an unlinked Git author is not guessed", carryOf: 12, wantCode: 5, wantReason: "not verifiably authored", change: func(r sequence.Routes) {
			r["GET /repos/o/r/compare/base123...abc123"] = []sequence.Response{{Body: map[string]any{"total_commits": 1, "commits": []any{carriedCommit("abc123", 0)}}}}
		}},
		{name: "case 4: empty comparisons prove no authorship", carryOf: 12, wantCode: 5, wantReason: "no carried commits", change: func(r sequence.Routes) {
			r["GET /repos/o/r/compare/base123...abc123"] = []sequence.Response{{Body: map[string]any{"total_commits": 0, "commits": []any{}}}}
		}},
		{name: "case 5: a queue cannot promise rebase", carryOf: 12, wantCode: 5, wantReason: "merge queue", change: func(r sequence.Routes) {
			r["GET /repos/o/r/rules/branches/main"] = []sequence.Response{{Body: []any{map[string]any{"type": "merge_queue"}}}}
		}},
		{name: "case 6: squash-only settings never change", carryOf: 12, wantCode: 3, wantReason: "--carry-of requires rebase", change: func(r sequence.Routes) {
			r["GET /repos/o/r"] = []sequence.Response{{Body: settings(false, true, false)}}
		}},
		{name: "case 7: the original author must be known", carryOf: 12, wantCode: 5, wantReason: "could not be identified", change: func(r sequence.Routes) {
			r["GET /repos/o/r/pulls/12"] = []sequence.Response{{Body: map[string]any{"user": nil}}}
		}},
		{name: "case 8: a carrier commit pushed during the wait is refused", carryOf: 12, wantCode: 5, wantReason: "not verifiably authored", waited: true, change: func(r sequence.Routes) {
			first := r["GET /repos/o/r/pulls/42"][0]
			next := pull(map[string]any{
				"base": map[string]any{"sha": "base123", "ref": "main", "repo": map[string]any{"full_name": "o/r"}},
				"head": map[string]any{"sha": "def456", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
			})
			r["GET /repos/o/r/pulls/42"] = []sequence.Response{first, {Body: next}}
			for key, responses := range greenHead("def456") {
				r[key] = responses
			}
			r["GET /repos/o/r/compare/base123...def456"] = []sequence.Response{{Body: map[string]any{"total_commits": 1, "commits": []any{carriedCommit("def456", 8)}}}}
		}},
		{name: "case 9: the existing human-author gate still applies", carryOf: 12, wantCode: 5, wantReason: "opened by bob", change: func(r sequence.Routes) {
			r["GET /repos/o/r/pulls/42"] = []sequence.Response{{Body: pull(map[string]any{"user": map[string]any{"login": "bob", "type": "User"}})}}
		}},
		{name: "case 10: a truncated comparison is a tooling failure", carryOf: 12, wantCode: -1, wantReason: "incomplete commit comparison", change: func(r sequence.Routes) {
			r["GET /repos/o/r/compare/base123...abc123"] = []sequence.Response{{Body: map[string]any{"total_commits": 2, "commits": []any{carriedCommit("abc123", 7)}}}}
		}},
		{name: "case 11: missing Git author data is refused", carryOf: 12, wantCode: 5, wantReason: "not verifiably authored", change: func(r sequence.Routes) {
			commit := carriedCommit("abc123", 7)
			commit["commit"] = map[string]any{}
			r["GET /repos/o/r/compare/base123...abc123"] = []sequence.Response{{Body: map[string]any{"total_commits": 1, "commits": []any{commit}}}}
		}},
		{name: "case 12: a push after the verdict cannot replace the judged head", carryOf: 12, wantCode: 5, wantReason: "head moved", waited: true, change: func(r sequence.Routes) {
			first := r["GET /repos/o/r/pulls/42"][0]
			next := pull(map[string]any{
				"base": map[string]any{"sha": "base123", "ref": "main", "repo": map[string]any{"full_name": "o/r"}},
				"head": map[string]any{"sha": "def456", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
			})
			r["GET /repos/o/r/pulls/42"] = []sequence.Response{first, first, {Body: next}}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := carryRoutes()
			if tc.change != nil {
				tc.change(r)
			}
			h := newHarness(t, r, func(c *Config) { c.CarryOf = tc.carryOf })
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.wantCode || (tc.wantReason != "" && (err == nil || !strings.Contains(err.Error(), tc.wantReason))) {
				t.Fatalf("want exit %d containing %q, got %d: %v", tc.wantCode, tc.wantReason, code, err)
			}
			waited := h.requested("GET /repos/o/r/commits/abc123/check-runs")+h.requested("GET /repos/o/r/commits/def456/check-runs") > 0
			if waited != tc.waited {
				t.Errorf("waited: got %v, want %v", waited, tc.waited)
			}
			if tc.wantCode != 0 {
				for _, request := range h.server.Requests() {
					if request.Method != http.MethodGet {
						t.Errorf("a refused carry wrote to GitHub: %s", request)
					}
				}
				return
			}
			if result.Method != "rebase" || result.CarryOf == nil || result.CarryOf.Author != "alice" || result.CarryOf.PullRequest != "https://github.com/o/r/pull/12" || result.MergedBy != "someone" || result.MergeCommitSHA != "m1" || !result.BranchDeleted {
				t.Errorf("carry outcome: %+v", result)
			}
			if n := h.requested("GET /repos/o/r/compare/base123...abc123"); n != 2 {
				t.Errorf("authorship must be checked before the wait and before merging, got %d reads", n)
			}
		})
	}
}

func Test_Merge_carryOfRebaseWire(t *testing.T) {
	server, err := githubmock.Start(carryRoutes())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httputil.NewSingleHostReverseProxy(target)
	var body map[string]any
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			_ = json.NewDecoder(r.Body).Decode(&body)
			r.Body, r.ContentLength = http.NoBody, 0
		}
		upstream.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	h := newHarnessAt(t, server, proxy.URL, func(c *Config) { c.CarryOf = 12 })
	if _, err := h.merge(t); err != nil {
		t.Fatal(err)
	}
	if body["sha"] != "abc123" || body["merge_method"] != "rebase" || body["commit_title"] != nil {
		t.Errorf("carry must preserve authors and subjects and pin the judged head: %v", body)
	}
}
