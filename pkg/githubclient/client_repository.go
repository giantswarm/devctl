package githubclient

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/github"
)

func (c *Client) ListRepositories(ctx context.Context, owner string) ([]Repository, error) {
	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listing repositories for owner %#q", owner))

	underlyingClient := c.getUnderlyingClient(ctx)

	var repos []Repository
	{
		opt := &github.RepositoryListByOrgOptions{
			ListOptions: github.ListOptions{
				PerPage: 500,
			},

			Type: "all",
		}
		for pageCnt := 0; ; pageCnt++ {
			c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listing page %d of repositories for owner %#q", pageCnt, owner))

			pageRepos, resp, err := underlyingClient.Repositories.ListByOrg(ctx, owner, opt)
			if err != nil {
				return nil, microerror.Mask(err)
			}

			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage

			for _, pageRepo := range pageRepos {
				r, err := newRepository(pageRepo, owner)
				if err != nil {
					return nil, microerror.Mask(err)
				}

				repos = append(repos, r)
			}

			c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listed page %d of %d repositories for owner %#q", pageCnt, len(pageRepos), owner))
		}
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("listed %d repositories for owner %#q", len(repos), owner))

	return repos, nil
}
