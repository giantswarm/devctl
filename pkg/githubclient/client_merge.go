package githubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// The calls `devctl pr merge` makes once the head is green: the merge itself,
// the branch update before it, the branch deletion after it, the merge queue.

// MergeMethod is how the head lands on the base.
type MergeMethod string

const (
	// MergeSquash lands the head as one commit.
	MergeSquash MergeMethod = "squash"
	// MergeRebase lands every commit of the head, rebased.
	MergeRebase MergeMethod = "rebase"
)

// MergeOptions configure [Client.MergePullRequest].
type MergeOptions struct {
	// Method is squash or rebase.
	Method MergeMethod
	// HeadSHA is the head the merge is for: GitHub refuses (409) when the
	// head moved since it was judged.
	HeadSHA string
	// CommitTitle is the squash commit's subject; empty leaves GitHub's
	// default. A rebase merge keeps the commits' own subjects.
	CommitTitle string
}

var mergeDeclinedError = &microerror.Error{
	Kind: "mergeDeclinedError",
}

// IsMergeDeclined asserts mergeDeclinedError: GitHub declined the merge as
// the pull request stands (405, a rule or the merge box blocks it; 409,
// the head moved since it was judged). Its message is GitHub's.
func IsMergeDeclined(err error) bool {
	return microerror.Cause(err) == mergeDeclinedError
}

// GitHub is the underlying go-github client, for the packages that take one
// (pkg/reposetup's Remote). It shares the token and the transport.
func (c *Client) GitHub() *github.Client {
	return c.ghClient
}

// CurrentLogin is the login the token acts as (GET /user).
func (c *Client) CurrentLogin(ctx context.Context) (string, error) {
	user, _, err := c.ghClient.Users.Get(ctx, "")
	if err != nil {
		return "", microerror.Mask(err)
	}
	return user.GetLogin(), nil
}

// MergePullRequest merges the pull request through the merge API and returns
// the merge commit's SHA. GitHub's refusal of the merge as the pull request
// stands is [IsMergeDeclined]; anything else is a tooling failure.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, opts MergeOptions) (string, error) {
	result, _, err := c.ghClient.PullRequests.Merge(ctx, owner, repo, number, "", &github.PullRequestOptions{
		SHA:         opts.HeadSHA,
		MergeMethod: string(opts.Method),
		CommitTitle: opts.CommitTitle,
	})
	switch status := githubStatus(err); {
	case err == nil:
	case status == http.StatusMethodNotAllowed, status == http.StatusConflict:
		return "", microerror.Maskf(mergeDeclinedError, "GitHub declined the merge of %s/%s#%d: %s", owner, repo, number, githubMessage(err))
	default:
		return "", microerror.Mask(err)
	}
	if !result.GetMerged() {
		return "", microerror.Maskf(mergeDeclinedError, "GitHub declined the merge of %s/%s#%d: %s", owner, repo, number, result.GetMessage())
	}
	return result.GetSHA(), nil
}

// UpdatePullRequestBranch asks GitHub to merge the base into the head
// (the "Update branch" button). GitHub schedules it and answers 202; the
// new head appears on the pull request a moment later. expectedHeadSHA
// guards against updating a head that moved meanwhile.
func (c *Client) UpdatePullRequestBranch(ctx context.Context, owner, repo string, number int, expectedHeadSHA string) error {
	_, _, err := c.ghClient.PullRequests.UpdateBranch(ctx, owner, repo, number, &github.PullRequestBranchUpdateOptions{
		ExpectedHeadSHA: &expectedHeadSHA,
	})
	var accepted *github.AcceptedError
	if err != nil && !errors.As(err, &accepted) {
		return microerror.Mask(err)
	}
	return nil
}

// DeleteBranch deletes refs/heads/branch through the refs API. A branch
// that is gone already (GitHub deleted it on merge, or answers 404) is not
// an error: the outcome is the same.
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	_, err := c.ghClient.Git.DeleteRef(ctx, owner, repo, "heads/"+branch)
	switch status := githubStatus(err); {
	case err == nil:
		return nil
	case status == http.StatusNotFound, status == http.StatusUnprocessableEntity:
		return nil
	default:
		return microerror.Mask(err)
	}
}

// MergeQueueRequired says whether a merge queue rule is in effect on branch:
// the rules endpoint of the branch lists one of type merge_queue. A
// repository whose rules the token may not read has none.
func (c *Client) MergeQueueRequired(ctx context.Context, owner, repo, branch string) (bool, error) {
	rules, _, err := c.ghClient.Repositories.ListRulesForBranch(ctx, owner, repo, branch, &github.ListOptions{PerPage: 100})
	switch status := githubStatus(err); {
	case err == nil:
		return len(rules.MergeQueue) > 0, nil
	case status == http.StatusNotFound, status == http.StatusForbidden:
		return false, nil
	default:
		return false, microerror.Mask(err)
	}
}

// EnqueuePullRequest adds the pull request to its base's merge queue: the
// GraphQL mutation enqueuePullRequest, REST has no call for it. The endpoint
// is <REST root>/graphql, api.github.com's layout. The queue merges when its
// checks pass; the caller waits on the pull request.
func (c *Client) EnqueuePullRequest(ctx context.Context, nodeID string) error {
	query := map[string]any{
		"query":     `mutation($id: ID!) { enqueuePullRequest(input: {pullRequestId: $id}) { mergeQueueEntry { id } } }`,
		"variables": map[string]any{"id": nodeID},
	}
	body, err := json.Marshal(query)
	if err != nil {
		return microerror.Mask(err)
	}
	url := strings.TrimRight(c.ghClient.BaseURL(), "/") + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return microerror.Mask(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.ghClient.Client().Do(req)
	if err != nil {
		return microerror.Mask(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return microerror.Mask(err)
	}
	if resp.StatusCode != http.StatusOK {
		return microerror.Maskf(executionError, "enqueue: GraphQL answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var answer struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return microerror.Maskf(executionError, "enqueue: GraphQL answered with no JSON: %s", strings.TrimSpace(string(raw)))
	}
	if len(answer.Errors) > 0 {
		messages := make([]string, 0, len(answer.Errors))
		for _, e := range answer.Errors {
			messages = append(messages, e.Message)
		}
		return microerror.Maskf(executionError, "enqueue: %s", strings.Join(messages, "; "))
	}
	return nil
}

// githubMessage is GitHub's message of an error response, or the error.
func githubMessage(err error) string {
	var errResponse *github.ErrorResponse
	if errors.As(err, &errResponse) && errResponse.Message != "" {
		return errResponse.Message
	}
	return fmt.Sprint(err)
}
