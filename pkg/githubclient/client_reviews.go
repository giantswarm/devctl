package githubclient

import (
	"context"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// PullRequestReviews returns every review of a pull request, oldest first,
// every page.
func (c *Client) PullRequestReviews(ctx context.Context, owner, repo string, number int) ([]*github.PullRequestReview, error) {
	opts := &github.ListOptions{PerPage: 100}
	var reviews []*github.PullRequestReview
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.PullRequests.ListReviews(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		reviews = append(reviews, result...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return reviews, nil
}

// PullRequestReviewComments returns every review comment of a pull request
// (the comments on its diff, replies included), every page.
func (c *Client) PullRequestReviewComments(ctx context.Context, owner, repo string, number int) ([]*github.PullRequestComment, error) {
	opts := &github.PullRequestListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	var comments []*github.PullRequestComment
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.PullRequests.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		comments = append(comments, result...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return comments, nil
}

// IssueComments returns every comment on the conversation of an issue or
// pull request, every page.
func (c *Client) IssueComments(ctx context.Context, owner, repo string, number int) ([]*github.IssueComment, error) {
	opts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	var comments []*github.IssueComment
	for page := 0; page < maxPages; page++ {
		result, resp, err := c.ghClient.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		comments = append(comments, result...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return comments, nil
}

// CommitTime returns the committer date of a commit, read through the git
// data API (no file list).
func (c *Client) CommitTime(ctx context.Context, owner, repo, sha string) (time.Time, error) {
	commit, _, err := c.ghClient.Git.GetCommit(ctx, owner, repo, sha)
	if err != nil {
		return time.Time{}, microerror.Mask(err)
	}
	return commit.GetCommitter().GetDate().Time, nil
}
