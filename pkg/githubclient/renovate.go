package githubclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/go-github/v60/github"
)

const (
	// Installation ID of Renovate in our organization. Corresponds to
	// https://github.com/organizations/giantswarm/settings/installations/17164699
	renovateInstallationID = 17164699

	githubApiVersion = "2022-11-28"
)

// Add repository to the Renovate installation. Corresponds to
// https://docs.github.com/en/rest/apps/installations?apiVersion=2022-11-28#add-a-repository-to-an-app-installation
func (c *Client) AddRepoToRenovatePermissions(ctx context.Context, org string, repo *github.Repository) error {
	path := fmt.Sprintf("/user/installations/%d/repositories/%d", renovateInstallationID, repo.GetID())
	realClient := c.getUnderlyingClient(ctx)

	req, err := realClient.NewRequest(http.MethodPut, path, nil, github.WithVersion(githubApiVersion))
	if err != nil {
		return err
	}

	resp, err := realClient.Do(ctx, req, nil)
	if err != nil {
		c.logger.Printf("response: %v", resp)
		return err
	}

	c.logger.Printf("response status: %q", resp.Status)

	return nil
}

// Remove repository from the Renovate installation. Corresponds to
// https://docs.github.com/en/rest/apps/installations?apiVersion=2022-11-28#remove-a-repository-from-an-app-installation
func (c *Client) RemoveRepoFromRenovatePermissions(ctx context.Context, org string, repo *github.Repository) error {
	path := fmt.Sprintf("/user/installations/%d/repositories/%d", renovateInstallationID, repo.GetID())
	realClient := c.getUnderlyingClient(ctx)

	req, err := realClient.NewRequest(http.MethodDelete, path, nil, github.WithVersion(githubApiVersion))
	if err != nil {
		return err
	}

	resp, err := realClient.Do(ctx, req, nil)
	if err != nil {
		c.logger.Printf("response: %v", resp)
		return err
	}

	c.logger.Printf("response status: %q", resp.Status)

	return nil
}
