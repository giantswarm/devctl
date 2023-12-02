package githubclient

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v57/github"
)

func (c *Client) GetFile(ctx context.Context, owner, repo, path, ref string) (RepositoryFile, error) {
	c.logger.Infof("getting %#q file content for owner %#q and repository %#q", path, owner, repo)

	underlyingClient := c.getUnderlyingClient(ctx)

	var file RepositoryFile
	{
		opt := &github.RepositoryContentGetOptions{
			Ref: ref,
		}

		fileContent, directoryContent, _, err := underlyingClient.Repositories.GetContents(ctx, owner, repo, path, opt)
		if isGithub404(err) {
			return RepositoryFile{}, microerror.Maskf(notFoundError, "repository file %#q for owner %#q in repository %#q", path, owner, repo)
		} else if err != nil {
			return RepositoryFile{}, microerror.Mask(err)
		}

		if directoryContent != nil {
			return RepositoryFile{}, microerror.Maskf(executionError, "expected file but content under path %#q is a directory", path)
		}

		file, err = newRepositoryFile(fileContent)
		if err != nil {
			return RepositoryFile{}, microerror.Mask(err)
		}
	}

	c.logger.Infof("got %#q file content for owner %#q and repository %#q", path, owner, repo)

	return file, nil
}
