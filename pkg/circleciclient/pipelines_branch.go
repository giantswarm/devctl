package circleciclient

import (
	"context"
	"net/url"

	"github.com/giantswarm/microerror"
)

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
