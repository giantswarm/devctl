package githubclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v44/github"
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

func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*github.Repository, error) {
	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("get repository %s/%s", owner, repo))

	underlyingClient := c.getUnderlyingClient(ctx)

	repository, response, err := underlyingClient.Repositories.Get(ctx, owner, repo)
	if err != nil {
		if response != nil && response.Response != nil && response.Response.StatusCode == http.StatusNotFound {
			return nil, microerror.Mask(notFoundError)
		}
		return nil, microerror.Mask(err)
	}

	c.logger.LogCtx(ctx, "level", "debug", "message", fmt.Sprintf("got repository %s", *repository.FullName))

	return repository, nil
}

func (c *Client) ConfigureRepository(ctx context.Context, owner, repo string) (*github.Repository, error) {
}
