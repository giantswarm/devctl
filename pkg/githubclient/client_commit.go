package githubclient

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/github"
)

func (c *Client) FindCommitByTag(ctx context.Context, owner string, repo string, tagName string) (github.Commit, error) {
	underlyingClient := c.getUnderlyingClient(ctx)

	tagRef := fmt.Sprintf("tags/%s", tagName)
	reference, _, err := underlyingClient.Git.GetRef(ctx, owner, repo, tagRef)
	if err != nil {
		return github.Commit{}, microerror.Maskf(err, "find commit tag ref failed", tagName)
	}

	tag, _, err := underlyingClient.Git.GetTag(ctx, owner, repo, *reference.Object.SHA)
	if err != nil {
		return github.Commit{}, microerror.Maskf(err, "find commit tag failed", *reference.Object.SHA)
	}

	commit, _, err := underlyingClient.Git.GetCommit(ctx, owner, repo, *tag.Object.SHA)
	if err != nil {
		return github.Commit{}, microerror.Maskf(err, "find commit failed", *tag.Object.SHA)
	}

	return *commit, nil
}
