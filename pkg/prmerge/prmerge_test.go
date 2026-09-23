package prmerge

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	githubmock "github.com/giantswarm/devctl/v8/e2e/mock/github"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// pull is o/r#42 as GitHub answers it: open, clean, opened by the caller,
// with over applied on top.
func pull(over map[string]any) map[string]any {
	pr := map[string]any{
		"number": 42, "state": "open", "draft": false, "mergeable_state": "clean",
		"title": "feat: thing", "node_id": "PR_1",
		"user": map[string]any{"login": "someone", "type": "User"},
		"head": map[string]any{"sha": "abc123", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}},
		"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "o/r"}},
	}
	maps.Copy(pr, over)
	return pr
}

// greenHead is a head with one passed check run and nothing else.
func greenHead(sha string) sequence.Routes {
	return sequence.Routes{
		"GET /repos/o/r/commits/" + sha + "/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
			map[string]any{"id": 1, "name": "go-build", "status": "completed", "conclusion": "success", "html_url": "https://github.com/o/r/runs/1"},
		}}}},
		"GET /repos/o/r/commits/" + sha + "/status":   {{Body: map[string]any{"state": "success", "total_count": 0, "statuses": []any{}}}},
		"GET /repos/o/r/actions/runs?head_sha=" + sha: {{Body: map[string]any{"total_count": 0, "workflow_runs": []any{}}}},
	}
}

var (
	merged  = sequence.Response{Body: map[string]any{"sha": "m1", "merged": true, "message": "Pull Request successfully merged"}}
	deleted = sequence.Response{Status: http.StatusNoContent}
)

// routes is the mock's script for a green o/r#42 that merges as a squash,
// with more applied on top.
func routes(pr map[string]any, more ...sequence.Routes) sequence.Routes {
	r := sequence.Routes{
		"GET /repos/o/r/pulls/42":                  {{Body: pr}},
		"PUT /repos/o/r/pulls/42/merge":            {merged},
		"DELETE /repos/o/r/git/refs/heads/feature": {deleted},
	}
	maps.Copy(r, greenHead("abc123"))
	for _, m := range more {
		maps.Copy(r, m)
	}
	return r
}

type harness struct {
	merger   *Merger
	server   *githubmock.Server
	progress *bytes.Buffer
}

func allow(context.Context, string, string) (Verdict, error) { return Verdict{}, nil }

func newHarness(t *testing.T, r sequence.Routes, configure func(*Config)) *harness {
	t.Helper()
	server, err := githubmock.Start(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	github, conditional, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	progress := &bytes.Buffer{}
	config := Config{
		Wait: prwait.Config{
			GitHub:   github,
			Rate:     conditional,
			Clock:    agentcli.NewClock(0.001, nil),
			Progress: agentcli.NewProgress(progress, true),
			Timeout:  2 * time.Minute,
		},
		Login:  "someone",
		Policy: allow,
	}
	if configure != nil {
		configure(&config)
	}
	merger, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{merger: merger, server: server, progress: progress}
}

func (h *harness) merge(t *testing.T) (*Result, error) {
	t.Helper()
	return h.merger.Merge(context.Background(), "o", "r", 42)
}

func (h *harness) requested(key string) int {
	n := 0
	for _, r := range h.server.Requests() {
		if r.Method+" "+r.Path == key {
			n++
		}
	}
	return n
}

func exitCode(err error) int {
	var exitErr *agentcli.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	if err == nil {
		return 0
	}
	return -1
}

func Test_Merge_refusalsBeforeTheWait(t *testing.T) {
	tests := []struct {
		name         string
		pr           map[string]any
		configure    func(*Config)
		wantCode     int
		wantReason   string
		wantVerdict  agentcli.Verdict
		wantRequests []string // routes that must have been requested
	}{
		{name: "closed", pr: pull(map[string]any{"state": "closed"}), wantCode: 3, wantReason: "is closed", wantVerdict: agentcli.VerdictNotApplicable},
		{name: "merged", pr: pull(map[string]any{"state": "closed", "merged": true}), wantCode: 3, wantReason: "is merged", wantVerdict: agentcli.VerdictNotApplicable},
		{name: "draft", pr: pull(map[string]any{"draft": true}), wantCode: 3, wantReason: "draft", wantVerdict: agentcli.VerdictNotApplicable},
		{name: "conflicting", pr: pull(map[string]any{"mergeable_state": "dirty"}), wantCode: 3, wantReason: "conflicts with main", wantVerdict: agentcli.VerdictNotApplicable},
		{name: "behind without --update-branch", pr: pull(map[string]any{"mergeable_state": "behind"}), wantCode: 3, wantReason: "behind main", wantVerdict: agentcli.VerdictNotApplicable},
		{name: "another human", pr: pull(map[string]any{"user": map[string]any{"login": "alice", "type": "User"}}), wantCode: 5, wantReason: "opened by alice, not by someone", wantVerdict: agentcli.VerdictRefused},
		{name: "opted out", pr: pull(nil), configure: func(c *Config) {
			c.Policy = func(context.Context, string, string) (Verdict, error) {
				return Verdict{Refusal: "the entry r says agentMerge: false"}, nil
			}
		}, wantCode: 5, wantReason: "agentMerge: false", wantVerdict: agentcli.VerdictRefused},
		{name: "state before author", pr: pull(map[string]any{"draft": true, "user": map[string]any{"login": "alice", "type": "User"}}), wantCode: 3, wantReason: "draft", wantVerdict: agentcli.VerdictNotApplicable},
		{name: "author before policy", pr: pull(map[string]any{"user": map[string]any{"login": "alice", "type": "User"}}), configure: func(c *Config) {
			c.Policy = func(context.Context, string, string) (Verdict, error) { return Verdict{Refusal: "opted out"}, nil }
		}, wantCode: 5, wantReason: "opened by alice", wantVerdict: agentcli.VerdictRefused},
		{name: "own pull request, login from GET /user", pr: pull(map[string]any{"user": map[string]any{"login": "Someone", "type": "User"}}), configure: func(c *Config) { c.Login = "" }, wantCode: 0, wantRequests: []string{"GET /user"}},
		{name: "a GitHub App", pr: pull(map[string]any{"user": map[string]any{"login": "renovate[bot]", "type": "Bot"}}), wantCode: 0},
		{name: "a [bot] login", pr: pull(map[string]any{"user": map[string]any{"login": "dependabot[bot]", "type": "User"}}), wantCode: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, routes(tc.pr, sequence.Routes{"GET /user": {{Body: map[string]any{"login": "someone"}}}}), tc.configure)
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, code, err)
			}
			if result.Repository != "o/r" || result.Number != 42 || result.HeadSHA != "abc123" || result.BaseRef != "main" {
				t.Errorf("the result names the pull request as read: %+v", result)
			}
			if tc.wantCode == 0 {
				if result.MergeCommitSHA != "m1" || !result.BranchDeleted {
					t.Errorf("want a merge, got %+v", result)
				}
				for _, key := range tc.wantRequests {
					if h.requested(key) == 0 {
						t.Errorf("want %s requested; got %v", key, h.server.Requests())
					}
				}
				return
			}
			var exitErr *agentcli.ExitError
			errors.As(err, &exitErr)
			if exitErr.Verdict != tc.wantVerdict || !strings.Contains(exitErr.Reason, tc.wantReason) {
				t.Errorf("want verdict %s with %q, got %s %q", tc.wantVerdict, tc.wantReason, exitErr.Verdict, exitErr.Reason)
			}
			for _, key := range []string{"GET /repos/o/r/commits/abc123/check-runs", "PUT /repos/o/r/pulls/42/merge", "DELETE /repos/o/r/git/refs/heads/feature"} {
				if h.requested(key) != 0 {
					t.Errorf("a refusal comes before any wait or merge; got %s", key)
				}
			}
			if result.MergeCommitSHA != "" || result.BranchDeleted || result.Enqueued {
				t.Errorf("nothing merged on a refusal: %+v", result)
			}
		})
	}
}

func Test_Merge_paths(t *testing.T) {
	type want struct {
		code      int
		reason    string
		method    string
		mergeSHA  string
		deleted   bool
		enqueued  bool
		headSHA   string
		requested map[string]int
		warning   string
	}
	tests := []struct {
		name      string
		routes    sequence.Routes
		configure func(*Config)
		want      want
	}{
		{
			name:   "green squash: merge with the head, delete the branch",
			routes: routes(pull(nil)),
			want: want{method: "squash", mergeSHA: "m1", deleted: true, headSHA: "abc123",
				requested: map[string]int{"PUT /repos/o/r/pulls/42/merge": 1, "DELETE /repos/o/r/git/refs/heads/feature": 1, "POST /graphql": 0}},
		},
		{
			name:      "rebase",
			routes:    routes(pull(nil)),
			configure: func(c *Config) { c.Method = githubclient.MergeRebase },
			want:      want{method: "rebase", mergeSHA: "m1", deleted: true, headSHA: "abc123", requested: map[string]int{"PUT /repos/o/r/pulls/42/merge": 1}},
		},
		{
			name: "a head in a fork is merged and its branch left alone",
			routes: routes(pull(map[string]any{
				"head": map[string]any{"sha": "abc123", "ref": "feature", "repo": map[string]any{"full_name": "alice/r"}},
			})),
			want: want{method: "squash", mergeSHA: "m1", deleted: false, headSHA: "abc123", warning: "fork alice/r",
				requested: map[string]int{"PUT /repos/o/r/pulls/42/merge": 1, "DELETE /repos/o/r/git/refs/heads/feature": 0}},
		},
		{
			name: "red: nothing merged",
			routes: routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
					map[string]any{"id": 1, "name": "go-test", "status": "completed", "conclusion": "failure", "html_url": "https://github.com/o/r/runs/1"},
				}}}},
			}),
			want: want{code: 1, reason: "go-test", method: "squash", headSHA: "abc123",
				requested: map[string]int{"PUT /repos/o/r/pulls/42/merge": 0, "DELETE /repos/o/r/git/refs/heads/feature": 0}},
		},
		{
			name: "a merge-queue base is enqueued, waited for, then the branch deleted",
			routes: routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/pulls/42": {
					{Body: pull(nil)}, {Body: pull(nil)}, {Body: pull(nil)}, {Body: pull(map[string]any{"mergeable_state": "blocked"})},
					{Body: pull(map[string]any{"state": "closed", "merged": true, "merge_commit_sha": "q1"})},
				},
				"GET /repos/o/r/rules/branches/main": {{Body: []any{
					map[string]any{"type": "merge_queue", "ruleset_source_type": "Repository", "ruleset_source": "o/r", "ruleset_id": 1, "parameters": map[string]any{"merge_method": "SQUASH"}},
				}}},
				"POST /graphql": {{Body: map[string]any{"data": map[string]any{"enqueuePullRequest": map[string]any{"mergeQueueEntry": map[string]any{"id": "MQE_1"}}}}}},
			}),
			want: want{method: "squash", mergeSHA: "q1", deleted: true, enqueued: true, headSHA: "abc123",
				requested: map[string]int{"POST /graphql": 1, "PUT /repos/o/r/pulls/42/merge": 0, "DELETE /repos/o/r/git/refs/heads/feature": 1}},
		},
		{
			name: "the merge queue drops the pull request: red",
			routes: routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/pulls/42":            {{Body: pull(nil)}, {Body: pull(nil)}, {Body: pull(nil)}, {Body: pull(map[string]any{"state": "closed"})}},
				"GET /repos/o/r/rules/branches/main": {{Body: []any{map[string]any{"type": "merge_queue", "ruleset_id": 1}}}},
				"POST /graphql":                      {{Body: map[string]any{"data": map[string]any{}}}},
			}),
			want: want{code: 1, reason: "merge queue removed", method: "squash", enqueued: true, headSHA: "abc123",
				requested: map[string]int{"DELETE /repos/o/r/git/refs/heads/feature": 0}},
		},
		{
			name: "behind with --update-branch: update, wait for the new head, merge",
			routes: routes(pull(nil), greenHead("def456"), sequence.Routes{
				"GET /repos/o/r/pulls/42": {
					{Body: pull(map[string]any{"mergeable_state": "behind"})},
					{Body: pull(map[string]any{"mergeable_state": "behind"})},
					{Body: pull(map[string]any{"head": map[string]any{"sha": "def456", "ref": "feature", "repo": map[string]any{"full_name": "o/r"}}})},
				},
				"PUT /repos/o/r/pulls/42/update-branch": {{Status: http.StatusAccepted, Body: map[string]any{"message": "Updating pull request branch.", "url": "https://api.github.com/repos/o/r/pulls/42"}}},
			}),
			configure: func(c *Config) { c.UpdateBranch = true },
			want: want{method: "squash", mergeSHA: "m1", deleted: true, headSHA: "def456",
				requested: map[string]int{"PUT /repos/o/r/pulls/42/update-branch": 1, "GET /repos/o/r/commits/def456/check-runs": 1, "GET /repos/o/r/commits/abc123/check-runs": 0, "PUT /repos/o/r/pulls/42/merge": 1}},
		},
		{
			name: "a mergeable state not computed yet is read again",
			routes: routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/pulls/42": {{Body: pull(map[string]any{"mergeable_state": "unknown"})}, {Body: pull(map[string]any{"mergeable_state": "behind"})}},
			}),
			want: want{code: 3, reason: "behind main", method: "squash", headSHA: "abc123", requested: map[string]int{"GET /repos/o/r/pulls/42": 2, "PUT /repos/o/r/pulls/42/merge": 0}},
		},
		{
			name: "GitHub declines the merge as the pull request stands: exit 3 with its sentence",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge": {{Status: http.StatusMethodNotAllowed, Body: map[string]any{"message": "Base branch was modified. Review and try the merge again.", "documentation_url": "https://docs.github.com/rest"}}},
			}),
			want: want{code: 3, reason: "Base branch was modified", method: "squash", headSHA: "abc123", requested: map[string]int{"DELETE /repos/o/r/git/refs/heads/feature": 0}},
		},
		{
			name: "the head moved under the merge: exit 3",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge": {{Status: http.StatusConflict, Body: map[string]any{"message": "Head branch was modified. Review and try the merge again."}}},
			}),
			want: want{code: 3, reason: "Head branch was modified", method: "squash", headSHA: "abc123"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.routes, tc.configure)
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.want.code {
				t.Fatalf("want exit %d, got %d: %v\n%s", tc.want.code, code, err, h.progress.String())
			}
			if tc.want.reason != "" {
				var exitErr *agentcli.ExitError
				if !errors.As(err, &exitErr) || !strings.Contains(exitErr.Reason, tc.want.reason) {
					t.Errorf("want reason with %q, got %v", tc.want.reason, err)
				}
			}
			if result.Method != tc.want.method || result.MergeCommitSHA != tc.want.mergeSHA || result.BranchDeleted != tc.want.deleted || result.Enqueued != tc.want.enqueued || result.HeadSHA != tc.want.headSHA {
				t.Errorf("want method %s, mergeCommitSha %q, branchDeleted %v, enqueued %v, headSha %s; got %+v", tc.want.method, tc.want.mergeSHA, tc.want.deleted, tc.want.enqueued, tc.want.headSHA, result)
			}
			for key, n := range tc.want.requested {
				if got := h.requested(key); got != n {
					t.Errorf("want %s requested %d time(s), got %d; requests: %v", key, n, got, h.server.Requests())
				}
			}
			if tc.want.warning != "" && !strings.Contains(strings.Join(result.Warnings, "\n"), tc.want.warning) {
				t.Errorf("want a warning with %q, got %v", tc.want.warning, result.Warnings)
			}
		})
	}
}

func Test_Merge_neverTouchesProtection(t *testing.T) {
	h := newHarness(t, routes(pull(nil)), nil)
	if _, err := h.merge(t); err != nil {
		t.Fatal(err)
	}
	for _, r := range h.server.Requests() {
		if r.Method == http.MethodGet {
			continue
		}
		switch r.Path {
		case "/repos/o/r/pulls/42/merge", "/repos/o/r/git/refs/heads/feature":
		default:
			t.Errorf("only the merge and the branch deletion write: %s", r)
		}
		if strings.Contains(r.Path, "protection") || strings.Contains(r.Path, "rulesets") {
			t.Errorf("no protection setting is written: %s", r)
		}
	}
}

func Test_Merge_reviewRuleDeclineNamesTheBypass(t *testing.T) {
	// GitHub's message, paragraphs and all.
	declined := sequence.Response{Status: http.StatusMethodNotAllowed, Body: map[string]any{
		"message": "Repository rule violations found\n\nAt least 1 approving review is required by reviewers with write access.\n\n",
	}}
	reviewRule := func(source string, id int) map[string]any {
		return map[string]any{"type": "pull_request", "ruleset_source_type": source, "ruleset_source": "o/r", "ruleset_id": id,
			"parameters": map[string]any{"required_approving_review_count": 1}}
	}
	ruleset := map[string]any{"id": 7, "name": "devctl: default branch", "source_type": "Repository", "source": "o/r", "enforcement": "active",
		"bypass_actors": []any{
			map[string]any{"actor_id": 5025978, "actor_type": "Integration", "bypass_mode": "pull_request"},
			map[string]any{"actor_id": 5176559, "actor_type": "Team", "bypass_mode": "pull_request"},
		}}
	owned := func(c *Config) {
		c.Policy = func(context.Context, string, string) (Verdict, error) { return Verdict{Team: "team-honeybadger"}, nil }
	}
	tests := []struct {
		name      string
		routes    sequence.Routes
		configure func(*Config)
		want      []string
		wantNot   string
		requested map[string]int
	}{
		{
			name: "a repository ruleset: the caller, its bypass actors, the owning team",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge":      {declined},
				"GET /repos/o/r/rules/branches/main": {{Body: []any{reviewRule("Repository", 7)}}},
				"GET /repos/o/r/rulesets/7":          {{Body: ruleset}},
			}),
			configure: owned,
			want: []string{
				"Repository rule violations found; At least 1 approving review is required by reviewers with write access. devctl acts as someone, who has no bypass on the ruleset \"devctl: default branch\" of o/r",
				"App 5025978 for pull requests, whose bypass covers its installation tokens and not the user token devctl acts with",
				"team 5176559 for pull requests",
				"the entry's owning team is team-honeybadger: one of its members or a repository admin merges it, or a reviewer with write access approves it first",
			},
			requested: map[string]int{"GET /repos/o/r/rulesets/7": 1, "DELETE /repos/o/r/git/refs/heads/feature": 0},
		},
		{
			name: "an organization ruleset is read from the organization; no team file, no team",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge":      {declined},
				"GET /repos/o/r/rules/branches/main": {{Body: []any{reviewRule("Organization", 9), reviewRule("Organization", 9)}}},
				"GET /orgs/o/rulesets/9": {{Body: map[string]any{"id": 9, "name": "org: reviews", "source_type": "Organization", "source": "o",
					"bypass_actors": []any{map[string]any{"actor_id": 1, "actor_type": "OrganizationAdmin", "bypass_mode": "always"}}}}},
			}),
			want: []string{
				"the ruleset \"org: reviews\" of the organization o (bypass actors: organization admins always)",
				"a repository admin or another bypass actor merges it, or a reviewer with write access approves it first",
			},
			requested: map[string]int{"GET /orgs/o/rulesets/9": 1},
		},
		{
			name: "no ruleset carries the rule: classic protection, aligned first",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge":      {declined},
				"GET /repos/o/r/rules/branches/main": {{Body: []any{}}},
			}),
			configure: owned,
			want:      []string{"the review rule of main, which no ruleset carries: classic branch protection requires the review", "aligned first", "team-honeybadger"},
		},
		{
			name: "a ruleset the token cannot read is said so",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge":      {declined},
				"GET /repos/o/r/rules/branches/main": {{Body: []any{reviewRule("Repository", 7)}}},
				"GET /repos/o/r/rulesets/7":          {{Status: http.StatusForbidden, Body: map[string]any{"message": "Resource not accessible by integration"}}},
			}),
			configure: owned,
			want:      []string{"the ruleset 7 of o/r (its bypass actors could not be read:", "Resource not accessible by integration", "team-honeybadger"},
		},
		{
			name: "a decline for another reason keeps GitHub's sentence alone",
			routes: routes(pull(nil), sequence.Routes{
				"PUT /repos/o/r/pulls/42/merge": {{Status: http.StatusMethodNotAllowed, Body: map[string]any{"message": "Base branch was modified. Review and try the merge again."}}},
				"GET /repos/o/r/rulesets/7":     {{Body: ruleset}},
			}),
			configure: owned,
			want:      []string{"Base branch was modified"},
			wantNot:   "devctl acts as",
			requested: map[string]int{"GET /repos/o/r/rulesets/7": 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.routes, tc.configure)
			result, err := h.merge(t)
			var exitErr *agentcli.ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != agentcli.ExitNotApplicable {
				t.Fatalf("want exit %d, got %v\n%s", agentcli.ExitNotApplicable, err, h.progress.String())
			}
			for _, w := range tc.want {
				if !strings.Contains(exitErr.Reason, w) {
					t.Errorf("want reason with %q, got %q", w, exitErr.Reason)
				}
			}
			if tc.wantNot != "" && strings.Contains(exitErr.Reason, tc.wantNot) {
				t.Errorf("want reason without %q, got %q", tc.wantNot, exitErr.Reason)
			}
			for key, n := range tc.requested {
				if got := h.requested(key); got != n {
					t.Errorf("want %s requested %d time(s), got %d; requests: %v", key, n, got, h.server.Requests())
				}
			}
			if result.MergeCommitSHA != "" || result.BranchDeleted {
				t.Errorf("nothing merged on a decline: %+v", result)
			}
			for _, r := range h.server.Requests() {
				if r.Method != http.MethodGet && strings.Contains(r.Path, "rulesets") {
					t.Errorf("the rulesets are read, never written: %s", r)
				}
			}
		})
	}
}

func Test_New_method(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	github, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Wait: prwait.Config{GitHub: github}, Method: "merge"}); !IsInvalidConfig(err) {
		t.Errorf("merge is not a method here: %v", err)
	}
	if _, err := New(Config{}); !IsInvalidConfig(err) {
		t.Errorf("GitHub is required: %v", err)
	}
	m, err := New(Config{Wait: prwait.Config{GitHub: github}})
	if err != nil {
		t.Fatal(err)
	}
	if m.method != githubclient.MergeSquash || m.timeout != prwait.DefaultTimeout || m.policy == nil {
		t.Errorf("defaults: %+v", m)
	}
}

func Test_TeamFilePolicy(t *testing.T) {
	teamFile := func(yaml string) sequence.Response {
		return sequence.Response{Body: map[string]any{
			"type": "file", "name": "team-a.yaml", "path": "repositories/team-a.yaml", "sha": "blob1",
			"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(yaml)),
		}}
	}
	dir := sequence.Response{Body: []any{map[string]any{"type": "file", "name": "team-a.yaml", "path": "repositories/team-a.yaml"}}}
	tests := []struct {
		name         string
		owner, repo  string
		routes       sequence.Routes
		wantRefusal  string
		wantTeam     string
		wantErr      bool
		wantRequests int
	}{
		{name: "agentMerge false refuses, naming the field", owner: "giantswarm", repo: "plans", routes: sequence.Routes{
			"GET /repos/giantswarm/github/contents/repositories":             {dir},
			"GET /repos/giantswarm/github/contents/repositories/team-a.yaml": {teamFile("- name: plans\n  componentType: service\n  agentMerge: false\n")},
		}, wantRefusal: "agentMerge: false", wantTeam: "team-a", wantRequests: 2},
		{name: "an entry without the field is not opted out; the verdict names the team", owner: "giantswarm", repo: "svc", routes: sequence.Routes{
			"GET /repos/giantswarm/github/contents/repositories":             {dir},
			"GET /repos/giantswarm/github/contents/repositories/team-a.yaml": {teamFile("- name: svc\n  agentMerge: true\n- name: other\n  agentMerge: false\n")},
		}, wantTeam: "team-a", wantRequests: 2},
		{name: "a repository no team file declares is not opted out", owner: "giantswarm", repo: "undeclared", routes: sequence.Routes{
			"GET /repos/giantswarm/github/contents/repositories":             {dir},
			"GET /repos/giantswarm/github/contents/repositories/team-a.yaml": {teamFile("- name: svc\n")},
		}, wantRequests: 2},
		{name: "a repository outside the organisation is not looked up", owner: "o", repo: "r", routes: sequence.Routes{}, wantRequests: 0},
		{name: "team files the token cannot read are an error, not a guess", owner: "giantswarm", repo: "svc", routes: sequence.Routes{}, wantErr: true, wantRequests: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, err := githubmock.Start(tc.routes)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.Close)
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			github, _, err := githubclient.NewConditional(githubclient.Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			verdict, err := TeamFilePolicy(github.GitHub())(context.Background(), tc.owner, tc.repo)
			if (err != nil) != tc.wantErr {
				t.Fatalf("want error %v, got %v", tc.wantErr, err)
			}
			if !strings.Contains(verdict.Refusal, tc.wantRefusal) || (tc.wantRefusal == "" && verdict.Refusal != "") {
				t.Errorf("want refusal %q, got %q", tc.wantRefusal, verdict.Refusal)
			}
			if verdict.Team != tc.wantTeam {
				t.Errorf("want team %q, got %q", tc.wantTeam, verdict.Team)
			}
			if n := len(server.Requests()); n != tc.wantRequests {
				t.Errorf("want %d request(s), got %d: %v", tc.wantRequests, n, server.Requests())
			}
		})
	}
}
