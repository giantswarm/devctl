package githubclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// wire records what one request carried.
type wire struct {
	method, path string
	body         map[string]any
}

func newWireClient(t *testing.T, status int, response string) (*Client, *wire) {
	t.Helper()
	got := &wire{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path = r.Method, r.URL.Path
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
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusConflict} {
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
