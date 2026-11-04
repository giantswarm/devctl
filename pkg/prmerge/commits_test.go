package prmerge

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

func commit(sha, message string) map[string]any {
	return map[string]any{"sha": sha, "commit": map[string]any{"message": message}}
}

func comparison(commits ...map[string]any) map[string]any {
	return map[string]any{"total_commits": len(commits), "commits": commits}
}

func Test_Merge_autosquashCommits(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		method     githubclient.MergeMethod
		fork       bool
		onlyRebase bool
		onlyMerge  bool
		acceptRed  bool
		wantRefuse bool
	}{
		{name: "case 0: explicit rebase refuses amend", message: "amend! fix(ui): render", method: githubclient.MergeRebase, wantRefuse: true},
		{name: "case 1: fork default refuses fixup", message: "fixup! fix(ui): render", fork: true, wantRefuse: true},
		{name: "case 2: settings select rebase and refuse squash", message: "squash! fix(ui): render", onlyRebase: true, wantRefuse: true},
		{name: "case 3: accept-red cannot bypass history", message: "amend! fix(ui): render", fork: true, acceptRed: true, wantRefuse: true},
		{name: "case 4: default squash is unaffected", message: "fixup! fix(ui): render"},
		{name: "case 5: explicit squash on a fork is unaffected", message: "amend! fix(ui): render", method: githubclient.MergeSquash, fork: true},
		{name: "case 6: clean rebase ignores body prefixes", message: "fix(ui): render\n\nfixup! is documented here", method: githubclient.MergeRebase},
		{name: "case 7: clean rebase accepts red as before", message: "fix(ui): render", fork: true, acceptRed: true},
		{name: "case 8: prefix later in subject is clean", message: "fix(ui): explain squash! commits", method: githubclient.MergeRebase},
		{name: "case 9: merge commits are unaffected", message: "squash! fix(ui): render", onlyMerge: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := routes(pull(map[string]any{"base": map[string]any{"ref": "main", "sha": "base123"}}), sequence.Routes{
				"GET /repos/o/r/compare/base123...abc123": {{Body: comparison(commit("abc123", tc.message))}},
			})
			if tc.onlyRebase {
				r["GET /repos/o/r"] = []sequence.Response{{Body: settings(false, false, true)}}
			}
			if tc.onlyMerge {
				r["GET /repos/o/r"] = []sequence.Response{{Body: settings(true, false, false)}}
			}
			if tc.acceptRed {
				for key, responses := range redE2E() {
					r[key] = responses
				}
			}
			h := newHarness(t, r, func(c *Config) {
				c.Method = tc.method
				c.Policy = verdict(Verdict{ForkLine: tc.fork})
				if tc.acceptRed {
					c.Wait.AcceptRed = []string{"e2e"}
				}
			})
			result, err := h.merge(t)
			if tc.wantRefuse {
				assertHistoryRefusal(t, h, result, err, "abc123 "+tc.message)
				if h.requested("GET /repos/o/r/commits/abc123/check-runs") != 0 {
					t.Error("autosquash commits must be refused before waiting for CI")
				}
				return
			}
			if err != nil || result.MergeCommitSHA != "m1" || !result.BranchDeleted {
				t.Fatalf("want a successful merge, got %+v, %v", result, err)
			}
			wantReads := 0
			if result.Method == "rebase" {
				wantReads = 2
			}
			if got := h.requested("GET /repos/o/r/compare/base123...abc123"); got != wantReads {
				t.Errorf("want %d history reads for %s, got %d", wantReads, result.Method, got)
			}
			if tc.acceptRed && (len(result.AcceptedRed) != 1 || result.AcceptedRed[0].Name != "e2e") {
				t.Errorf("want e2e accepted and recorded, got %v", result.AcceptedRed)
			}
		})
	}
}

func assertHistoryRefusal(t *testing.T, h *harness, result *Result, err error, reasons ...string) {
	t.Helper()
	if code, outcome := agentcli.Outcome(err); code != agentcli.ExitNotApplicable || outcome != agentcli.VerdictNotApplicable {
		t.Fatalf("want not_applicable (exit 3), got %v", err)
	}
	for _, reason := range append([]string{"rebase", "autosquash", "--accept-red"}, reasons...) {
		if !strings.Contains(err.Error(), reason) {
			t.Errorf("want reason containing %q, got %v", reason, err)
		}
	}
	assertHistoryNoWrites(t, h, result)
}

func assertHistoryNoWrites(t *testing.T, h *harness, result *Result) {
	t.Helper()
	if result.MergeCommitSHA != "" || result.BranchDeleted || result.Enqueued {
		t.Errorf("a history refusal must not merge, enqueue or delete: %+v", result)
	}
	for _, request := range h.server.Requests() {
		if request.Method != http.MethodGet {
			t.Errorf("a history refusal must not write: %s", request)
		}
	}
}

func Test_Merge_autosquashEveryPage(t *testing.T) {
	// A large PR must not inherit the pull-request commits endpoint's
	// 250-commit limit, or stop after the comparison's first page.
	first := make([]map[string]any, 100)
	second := make([]map[string]any, 100)
	third := make([]map[string]any, 51)
	for i := range 251 {
		c := commit(fmt.Sprintf("c%d", i), "fix: clean")
		switch {
		case i < 100:
			first[i] = c
		case i < 200:
			second[i-100] = c
		default:
			third[i-200] = c
		}
	}
	third[48] = commit("amend-sha", "amend! fix: one\n\nbody must not be listed")
	third[49] = commit("fixup-sha", "fixup! fix: two")
	third[50] = commit("abc123", "squash! fix: three")
	r := routes(pull(map[string]any{"base": map[string]any{"ref": "main", "sha": "base123"}}))
	for i, page := range [][]map[string]any{first, second, third} {
		response := sequence.Response{Body: map[string]any{"total_commits": 251, "commits": page}}
		if i < 2 {
			response.Headers = map[string]string{"Link": fmt.Sprintf("</repos/o/r/compare/base123...abc123?page=%d&per_page=100>; rel=\"next\"", i+2)}
		}
		r[fmt.Sprintf("GET /repos/o/r/compare/base123...abc123?page=%d", i+1)] = []sequence.Response{response}
	}
	h := newHarness(t, r, func(c *Config) { c.Method = githubclient.MergeRebase })
	result, err := h.merge(t)
	assertHistoryRefusal(t, h, result, err, "amend-sha amend! fix: one", "fixup-sha fixup! fix: two", "abc123 squash! fix: three")
	if strings.Contains(err.Error(), "body must not be listed") {
		t.Error("the refusal must list subjects, not full commit messages")
	}
	if h.requested("GET /repos/o/r/compare/base123...abc123") != 3 {
		t.Error("want every comparison page read")
	}
}

func Test_Merge_autosquashNewHeadAfterWait(t *testing.T) {
	pr := pull(map[string]any{"base": map[string]any{"ref": "main", "sha": "base123"}})
	updated := pull(map[string]any{
		"base": map[string]any{"ref": "main", "sha": "base123"},
		"head": map[string]any{"sha": "def456", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
	})
	h := newHarness(t, routes(pr, greenHead("def456"), sequence.Routes{
		"GET /repos/o/r/pulls/42":                 {{Body: pr}, {Body: updated}},
		"GET /repos/o/r/compare/base123...abc123": {{Body: comparison(commit("abc123", "fix: clean"))}},
		"GET /repos/o/r/compare/base123...def456": {{Body: comparison(commit("def456", "fixup! fix: clean"))}},
	}), func(c *Config) { c.Method = githubclient.MergeRebase })
	result, err := h.merge(t)
	assertHistoryRefusal(t, h, result, err, "def456 fixup! fix: clean")
	if result.HeadSHA != "def456" || h.requested("GET /repos/o/r/commits/def456/check-runs") != 1 {
		t.Errorf("want the changed, judged head checked before merge, got %+v", result)
	}
}

func Test_Merge_autosquashReadFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response sequence.Response
	}{
		{name: "case 0: forbidden", response: sequence.Response{Status: http.StatusForbidden, Body: map[string]any{"message": "cannot read commits"}}},
		{name: "case 1: incomplete", response: sequence.Response{Body: map[string]any{"total_commits": 2, "commits": []any{commit("abc123", "fix: clean")}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, routes(pull(map[string]any{"base": map[string]any{"ref": "main", "sha": "base123"}}), sequence.Routes{
				"GET /repos/o/r/compare/base123...abc123": {tc.response},
			}), func(c *Config) { c.Method = githubclient.MergeRebase })
			result, err := h.merge(t)
			if err == nil {
				t.Fatal("unreadable or incomplete history must fail closed")
			}
			assertHistoryNoWrites(t, h, result)
			if h.requested("GET /repos/o/r/commits/abc123/check-runs") != 0 {
				t.Error("must not wait when history could not be checked")
			}
		})
	}
}
