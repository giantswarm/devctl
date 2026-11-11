package githubclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// StalePipeline checks current branch and PR state. Default-branch and tag
// pipelines are protected. A missing branch alone is not evidence of staleness.
func (c *Client) StalePipeline(ctx context.Context, owner, repo string, p circleciclient.Pipeline) (bool, error) {
	repository, _, err := c.ghClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return false, err
	}
	branch := p.VCS.Branch
	if branch == "" || p.VCS.Tag != "" || p.VCS.Revision == "" || branch == repository.GetDefaultBranch() {
		return false, nil
	}
	if strings.HasPrefix(branch, "pull/") {
		number, err := strconv.Atoi(strings.TrimPrefix(branch, "pull/"))
		if err != nil || number <= 0 {
			return false, nil
		}
		pr, _, err := c.ghClient.PullRequests.Get(ctx, owner, repo, number)
		if err != nil {
			return false, err
		}
		if pr.GetHead().GetSHA() == "" {
			return false, nil
		}
		return pr.GetState() == "closed" || pr.GetHead().GetSHA() != p.VCS.Revision, nil
	}
	// An open PR always wins over a closed historical PR for a reused branch.
	opts := &github.PullRequestListOptions{State: "all", Head: owner + ":" + branch, ListOptions: github.ListOptions{PerPage: 100}}
	closed := false
	for {
		prs, response, err := c.ghClient.PullRequests.List(ctx, owner, repo, opts)
		if err != nil {
			return false, err
		}
		for _, pr := range prs {
			if pr.GetHead().GetRef() != branch || !strings.EqualFold(pr.GetHead().GetRepo().GetFullName(), owner+"/"+repo) {
				continue
			}
			if pr.GetState() == "open" {
				if pr.GetHead().GetSHA() == "" {
					return false, fmt.Errorf("GitHub returned an empty PR head")
				}
				return pr.GetHead().GetSHA() != p.VCS.Revision, nil
			}
			if pr.GetHead().GetSHA() == p.VCS.Revision {
				closed = true
			}
		}
		if response.NextPage == 0 {
			break
		}
		opts.Page = response.NextPage
	}
	ref, _, err := c.ghClient.Git.GetRef(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		if r, ok := err.(*github.ErrorResponse); ok && r.Response.StatusCode == 404 {
			return closed, nil
		}
		return false, err
	}
	if ref.GetObject().GetSHA() == "" {
		return false, fmt.Errorf("GitHub returned an empty branch head")
	}
	// A reused branch with a new head is stale by revision; a current head is
	// retained unless that revision belongs to a closed PR.
	return ref.GetObject().GetSHA() != p.VCS.Revision || closed, nil
}
