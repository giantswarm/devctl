package circleciclient

import (
	"context"
	"fmt"
	"net/url"

	"github.com/giantswarm/microerror"
)

// RevisionPipelinePages bounds the search of FindPipelineByRevision: the
// pipeline of a pull request's head is among its branch's newest.
const RevisionPipelinePages = 3

// PullRequestBranch is the branch CircleCI builds a pull request's head as:
// the head's branch name, or "pull/<number>" for a head in a fork.
func PullRequestBranch(headRef string, fork bool, number int) string {
	if fork {
		return fmt.Sprintf("pull/%d", number)
	}
	return headRef
}

// ListBranchPipelines returns one page of the pipelines of branch, newest
// first: the most recent ones for an empty pageToken, the page after a page
// for its NextPageToken. The branch of a pull request from a fork is
// "pull/<number>". CircleCI lists pipelines by branch only; the caller picks
// the one of a revision from Pipeline.VCS.Revision.
func (c *Client) ListBranchPipelines(ctx context.Context, org, repo, branch, pageToken string) (*PipelinePage, error) {
	query := url.Values{"branch": {branch}}
	if pageToken != "" {
		query.Set("page-token", pageToken)
	}
	var page PipelinePage
	if err := c.do(ctx, "GET", c.v2Project(org, repo)+"/pipeline?"+query.Encode(), nil, &page); err != nil {
		return nil, microerror.Mask(err)
	}
	return &page, nil
}

// FindPipelineByRevision returns the newest pipeline of branch that built
// revision, or nil when the branch's newest [RevisionPipelinePages] pages do
// not include one: CircleCI has not started it yet, or never will.
func (c *Client) FindPipelineByRevision(ctx context.Context, org, repo, branch, revision string) (*Pipeline, error) {
	pageToken := ""
	for page := 0; page < RevisionPipelinePages; page++ {
		pipelines, err := c.ListBranchPipelines(ctx, org, repo, branch, pageToken)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		for i := range pipelines.Items {
			if pipelines.Items[i].VCS.Revision == revision {
				return &pipelines.Items[i], nil
			}
		}
		if pipelines.NextPageToken == "" {
			break
		}
		pageToken = pipelines.NextPageToken
	}
	return nil, nil
}
