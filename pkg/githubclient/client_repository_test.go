package githubclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v92/github"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// newRoutesClient is a client whose server answers each path with its
// route's body, and 404 for any other path.
func newRoutesClient(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	c, _, err := NewConditional(Config{Logger: logger, AccessToken: "ghu_test", BaseURL: server.URL})
	require.NoError(t, err)
	return c
}

func testRepository() *github.Repository {
	return &github.Repository{Owner: &github.User{Login: new("o")}, Name: new("r")}
}

// TestReportedChecksNewestMerge pins the head the checks are read from: the
// newest merge, not the most recently updated pull request. A long-merged
// pull request an unassignment moved to the top of the list, whose head
// predates the checks, stays behind the newest merge.
func TestReportedChecksNewestMerge(t *testing.T) {
	c := newRoutesClient(t, map[string]string{
		"/repos/o/r/pulls": `[
			{"number":220,"merged_at":"2024-04-29T08:15:59Z","updated_at":"2026-09-29T21:06:02Z","head":{"sha":"old"}},
			{"number":421,"merged_at":null,"updated_at":"2026-09-29T10:00:00Z","head":{"sha":"closed"}},
			{"number":420,"merged_at":"2026-09-28T10:27:26Z","updated_at":"2026-09-28T10:27:28Z","head":{"sha":"new"}}
		]`,
		"/repos/o/r/commits/new/status":     `{"statuses":[{"context":"ci/circleci: build-chart"}]}`,
		"/repos/o/r/commits/new/check-runs": `{"total_count":1,"check_runs":[{"name":"pre-commit","conclusion":"success"}]}`,
		"/repos/o/r/commits/old/status":     `{"statuses":[]}`,
		"/repos/o/r/commits/old/check-runs": `{"total_count":0,"check_runs":[]}`,
	})
	checks, err := c.ReportedChecks(t.Context(), testRepository(), "main")
	require.NoError(t, err)
	require.Equal(t, []string{"ci/circleci: build-chart", "pre-commit"}, checks)
}

// TestReportedChecksNoneOnHead pins that a head without any check is no
// evidence: the answer is nothing reported, never an empty list that would
// remove every required check.
func TestReportedChecksNoneOnHead(t *testing.T) {
	c := newRoutesClient(t, map[string]string{
		"/repos/o/r/pulls":                  `[{"number":220,"merged_at":"2024-04-29T08:15:59Z","head":{"sha":"old"}}]`,
		"/repos/o/r/commits/old/status":     `{"statuses":[]}`,
		"/repos/o/r/commits/old/check-runs": `{"total_count":0,"check_runs":[]}`,
	})
	_, err := c.ReportedChecks(t.Context(), testRepository(), "main")
	require.True(t, IsNotFound(err), "want notFoundError, got %v", err)
}

// TestReportedChecksSkipsPushMergedHead pins the fork-line case: the newest
// merge is an upstream re-pin whose head landed on the branch by a push (its
// merge commit is its head), so its SHA carries the branch's push runs and
// statuses and no pull-request check. It is passed over for the newest merge
// that went through the gate; nothing of the pushed head is reported.
func TestReportedChecksSkipsPushMergedHead(t *testing.T) {
	c := newRoutesClient(t, map[string]string{
		"/repos/o/r/pulls": `[
			{"number":119,"merged_at":"2026-10-05T20:46:48Z","merge_commit_sha":"pushed","head":{"sha":"pushed"}},
			{"number":118,"merged_at":"2026-10-05T08:19:59Z","merge_commit_sha":"squash118","head":{"sha":"gated"}}
		]`,
		"/repos/o/r/commits/pushed/status":     `{"statuses":[{"context":"ci/circleci: push-ateapi"}]}`,
		"/repos/o/r/commits/pushed/check-runs": `{"total_count":1,"check_runs":[{"name":"Tag","conclusion":"success"}]}`,
		"/repos/o/r/commits/gated/status":      `{"statuses":[{"context":"ci/circleci: build-ateapi"}]}`,
		"/repos/o/r/commits/gated/check-runs":  `{"total_count":1,"check_runs":[{"name":"semantic-pull-request / Validate PR title","conclusion":"success"}]}`,
	})
	checks, err := c.ReportedChecks(t.Context(), testRepository(), "giantswarm")
	require.NoError(t, err)
	require.Equal(t, []string{"ci/circleci: build-ateapi", "semantic-pull-request / Validate PR title"}, checks)
}

// TestReportedChecksOnlyPushMerged pins that merges by push alone are no
// evidence: nothing has reported, and no required check is removed.
func TestReportedChecksOnlyPushMerged(t *testing.T) {
	c := newRoutesClient(t, map[string]string{
		"/repos/o/r/pulls":                     `[{"number":119,"merged_at":"2026-10-05T20:46:48Z","merge_commit_sha":"pushed","head":{"sha":"pushed"}}]`,
		"/repos/o/r/commits/pushed/status":     `{"statuses":[{"context":"ci/circleci: push-ateapi"}]}`,
		"/repos/o/r/commits/pushed/check-runs": `{"total_count":1,"check_runs":[{"name":"Tag","conclusion":"success"}]}`,
	})
	_, err := c.ReportedChecks(t.Context(), testRepository(), "giantswarm")
	require.True(t, IsNotFound(err), "want notFoundError, got %v", err)
}
