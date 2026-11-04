package githubclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// wire records what one request carried.
type wire struct {
	method, path, query string
	body                map[string]any
}

func newWireClient(t *testing.T, status int, response string) (*Client, *wire) {
	t.Helper()
	got := &wire{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &got.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c, got
}

func Test_MergePullRequest_wire(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"sha":"m1","merged":true,"message":"Pull Request successfully merged"}`)
	sha, err := c.MergePullRequest(context.Background(), "o", "r", 42, MergeOptions{Method: MergeSquash, HeadSHA: "abc123", CommitTitle: "feat: thing (#42)"})
	if err != nil || sha != "m1" {
		t.Fatalf("want m1, got %q, %v", sha, err)
	}
	if got.method != http.MethodPut || got.path != "/repos/o/r/pulls/42/merge" {
		t.Errorf("want PUT /repos/o/r/pulls/42/merge, got %s %s", got.method, got.path)
	}
	for key, want := range map[string]any{"sha": "abc123", "merge_method": "squash", "commit_title": "feat: thing (#42)"} {
		if got.body[key] != want {
			t.Errorf("%s: want %v, got %v", key, want, got.body[key])
		}
	}
}

func Test_MergePullRequest_declined(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusConflict, http.StatusForbidden} {
		c, _ := newWireClient(t, status, `{"message":"Base branch was modified. Review and try the merge again.","documentation_url":"https://docs.github.com/rest"}`)
		_, err := c.MergePullRequest(context.Background(), "o", "r", 42, MergeOptions{Method: MergeSquash, HeadSHA: "abc123"})
		if !IsMergeDeclined(err) || !strings.Contains(err.Error(), "Base branch was modified") {
			t.Errorf("%d: want a declined merge with GitHub's sentence, got %v", status, err)
		}
	}
	c, _ := newWireClient(t, http.StatusInternalServerError, `{"message":"boom"}`)
	if _, err := c.MergePullRequest(context.Background(), "o", "r", 42, MergeOptions{}); err == nil || IsMergeDeclined(err) {
		t.Errorf("a 500 is a tooling failure, got %v", err)
	}
}

func Test_UpdatePullRequestBranch_acceptsThe202(t *testing.T) {
	c, got := newWireClient(t, http.StatusAccepted, `{"message":"Updating pull request branch.","url":"https://api.github.com/repos/o/r/pulls/42"}`)
	if err := c.UpdatePullRequestBranch(context.Background(), "o", "r", 42, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPut || got.path != "/repos/o/r/pulls/42/update-branch" || got.body["expected_head_sha"] != "abc123" {
		t.Errorf("want PUT update-branch with the expected head, got %s %s %v", got.method, got.path, got.body)
	}
}

func Test_RebasePullRequestBranch_wire(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"data":{"updatePullRequestBranch":{"pullRequest":{"id":"PR_1"}}}}`)
	if err := c.RebasePullRequestBranch(context.Background(), "PR_1", "abc123"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/graphql" {
		t.Errorf("want POST /graphql, got %s %s", got.method, got.path)
	}
	if query, _ := got.body["query"].(string); !strings.Contains(query, "updatePullRequestBranch") || !strings.Contains(query, "updateMethod: REBASE") {
		t.Errorf("want updatePullRequestBranch with updateMethod REBASE, got %v", got.body["query"])
	}
	if variables, _ := got.body["variables"].(map[string]any); variables["id"] != "PR_1" || variables["head"] != "abc123" {
		t.Errorf("variables: %v", got.body["variables"])
	}

	c, _ = newWireClient(t, http.StatusOK, `{"data":null,"errors":[{"message":"expected head sha didn't match current head ref"}]}`)
	if err := c.RebasePullRequestBranch(context.Background(), "PR_1", "abc123"); err == nil || !strings.Contains(err.Error(), "update-branch: expected head sha") {
		t.Errorf("GraphQL errors are errors naming GitHub's message, got %v", err)
	}
}

func Test_RebasePullRequestBranch_errors(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		spent        bool
		enqueue      bool
		wantConflict bool
		wantReason   string
	}{
		{name: "case 0: rebase conflict", body: `{"errors":[{"message":"Rebase failed due to conflicts"}]}`, wantConflict: true, wantReason: "Rebase failed due to conflicts"},
		{name: "case 1: GitHub CLI conflict", body: `{"errors":[{"type":"UNPROCESSABLE","path":["updatePullRequestBranch"],"message":"merge conflict between base and head"}]}`, wantConflict: true, wantReason: "merge conflict between base and head"},
		{name: "case 2: conflict with mutation suffix", body: `{"errors":[{"message":"merge conflict between base and head (updatePullRequestBranch)"}]}`, wantConflict: true, wantReason: "merge conflict between base and head"},
		{name: "case 3: stale expected head", body: `{"errors":[{"type":"UNPROCESSABLE","message":"expected head sha didn't match current head ref"}]}`, wantReason: "expected head sha"},
		{name: "case 4: permission refusal", body: `{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`, wantReason: "Resource not accessible"},
		{name: "case 5: rate limit", body: `{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`, wantReason: "API rate limit exceeded"},
		{name: "case 6: spent limit with conflict text", body: `{"errors":[{"message":"Rebase failed due to conflicts"}]}`, spent: true, wantReason: "the rate limit is spent"},
		{name: "case 7: conflict alongside another failure", body: `{"errors":[{"message":"Rebase failed due to conflicts"},{"message":"something else failed"}]}`, wantReason: "Rebase failed due to conflicts; something else failed"},
		{name: "case 8: unrelated error mentioning conflicts", body: `{"errors":[{"message":"could not read conflict data"}]}`, wantReason: "could not read conflict data"},
		{name: "case 9: HTTP refusal", status: http.StatusUnprocessableEntity, body: `{"errors":[{"message":"Rebase failed due to conflicts"}]}`, wantReason: "GraphQL answered 422"},
		{name: "case 10: server failure", status: http.StatusServiceUnavailable, body: `unavailable`, wantReason: "GraphQL answered 503"},
		{name: "case 11: malformed response", body: `not JSON`, wantReason: "GraphQL answered with no JSON"},
		{name: "case 12: enqueue preserves its error", body: `{"errors":[{"message":"Rebase failed due to conflicts"}]}`, enqueue: true, wantReason: "enqueue: Rebase failed due to conflicts"},
		{name: "case 13: authentication refusal", status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`, wantReason: "GraphQL answered 401"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if tc.spent {
					w.Header().Set("X-RateLimit-Remaining", "0")
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			if tc.enqueue {
				err = c.EnqueuePullRequest(context.Background(), "PR_1")
			} else {
				err = c.RebasePullRequestBranch(context.Background(), "PR_1", "abc123")
			}
			if err == nil || IsBranchUpdateConflict(err) != tc.wantConflict || !strings.Contains(err.Error(), tc.wantReason) {
				t.Fatalf("want conflict %v with %q, got %v", tc.wantConflict, tc.wantReason, err)
			}
			if !tc.wantConflict && !IsExecution(err) {
				t.Errorf("want the original execution error, got %v", err)
			}
		})
	}
}

func Test_RebasePullRequestBranch_canceled(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.RebasePullRequestBranch(ctx, "PR_1", "abc123")
	if !errors.Is(err, context.Canceled) || IsBranchUpdateConflict(err) {
		t.Errorf("want cancellation, got %v", err)
	}
	if got.method != "" {
		t.Errorf("cancellation must prevent the request, got %s", got.method)
	}
}

func Test_DeleteBranch_goneIsFine(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusUnprocessableEntity} {
		c, got := newWireClient(t, status, `{"message":"Reference does not exist"}`)
		if err := c.DeleteBranch(context.Background(), "o", "r", "feature"); err != nil {
			t.Errorf("%d: %v", status, err)
		}
		if got.method != http.MethodDelete || got.path != "/repos/o/r/git/refs/heads/feature" {
			t.Errorf("want DELETE /repos/o/r/git/refs/heads/feature, got %s %s", got.method, got.path)
		}
	}
}

func Test_MergeQueueRequired(t *testing.T) {
	c, _ := newWireClient(t, http.StatusOK, `[{"type":"merge_queue","ruleset_id":1,"parameters":{"merge_method":"SQUASH"}},{"type":"pull_request","ruleset_id":1,"parameters":{}}]`)
	if queued, err := c.MergeQueueRequired(context.Background(), "o", "r", "main"); err != nil || !queued {
		t.Errorf("want a merge queue, got %v, %v", queued, err)
	}
	c, _ = newWireClient(t, http.StatusOK, `[{"type":"pull_request","ruleset_id":1,"parameters":{}}]`)
	if queued, err := c.MergeQueueRequired(context.Background(), "o", "r", "main"); err != nil || queued {
		t.Errorf("want no merge queue, got %v, %v", queued, err)
	}
	c, _ = newWireClient(t, http.StatusNotFound, `{"message":"Not Found"}`)
	if queued, err := c.MergeQueueRequired(context.Background(), "o", "r", "main"); err != nil || queued {
		t.Errorf("rules the token cannot read are none, got %v, %v", queued, err)
	}
}

func Test_EnqueuePullRequest_wire(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"data":{"enqueuePullRequest":{"mergeQueueEntry":{"id":"MQE_1"}}}}`)
	if err := c.EnqueuePullRequest(context.Background(), "PR_1"); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/graphql" {
		t.Errorf("want POST /graphql, got %s %s", got.method, got.path)
	}
	if query, _ := got.body["query"].(string); !strings.Contains(query, "enqueuePullRequest") {
		t.Errorf("query: %v", got.body["query"])
	}
	if variables, _ := got.body["variables"].(map[string]any); variables["id"] != "PR_1" {
		t.Errorf("variables: %v", got.body["variables"])
	}

	c, _ = newWireClient(t, http.StatusOK, `{"data":null,"errors":[{"message":"The pull request is already in the merge queue"}]}`)
	if err := c.EnqueuePullRequest(context.Background(), "PR_1"); err == nil || !strings.Contains(err.Error(), "already in the merge queue") {
		t.Errorf("GraphQL errors are errors, got %v", err)
	}
}

func Test_BehindBy_wire(t *testing.T) {
	c, got := newWireClient(t, http.StatusOK, `{"status":"diverged","ahead_by":1,"behind_by":2}`)
	by, err := c.BehindBy(context.Background(), "o", "r", "main", "abc123")
	if err != nil || by != 2 {
		t.Fatalf("want 2, got %d, %v", by, err)
	}
	if got.method != http.MethodGet || got.path != "/repos/o/r/compare/main...abc123" {
		t.Errorf("want GET /repos/o/r/compare/main...abc123, got %s %s", got.method, got.path)
	}
}

func Test_BehindBy_failure(t *testing.T) {
	c, _ := newWireClient(t, http.StatusNotFound, `{"message":"Not Found"}`)
	if _, err := c.BehindBy(context.Background(), "o", "r", "main", "gone"); err == nil {
		t.Fatal("want an error for a comparison GitHub cannot make")
	}
}

func Test_IsMergeRetryable(t *testing.T) {
	for status, want := range map[int]bool{http.StatusBadGateway: true, http.StatusInternalServerError: true, http.StatusUnauthorized: false, http.StatusNotFound: false, http.StatusConflict: false} {
		c, _ := newWireClient(t, status, `{"message":"x"}`)
		_, err := c.MergePullRequest(context.Background(), "o", "r", 42, MergeOptions{})
		if got := IsMergeRetryable(err); got != want {
			t.Errorf("%d: want %v, got %v", status, want, got)
		}
	}
}
