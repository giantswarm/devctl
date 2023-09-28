package githubclient

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v55/github"
)

const (
	// App ID of the renovate installation.
	renovateAppID = 2740

	githubApiVersion = "2022-11-28"
)

func (c *Client) GetRenovateInstallation(ctx context.Context, org string) (*github.Installation, error) {
	// Get app installations to detect the Renovate app.
	realClient := c.getUnderlyingClient(ctx)
	installations, _, err := realClient.Organizations.ListInstallations(ctx, org, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	for _, inst := range installations.Installations {
		if *inst.AppID == renovateAppID {
			return inst, nil
		}
	}

	return nil, microerror.Mask(installationNotFoundError)
}

func (c *Client) AddRepoToRenovatePermissions(ctx context.Context, org string, repo *github.Repository) error {
	inst, err := c.GetRenovateInstallation(ctx, org)
	if err != nil {
		return microerror.Mask(err)
	}

	path := fmt.Sprintf("/user/installations/%d/repositories/%d", inst.GetID(), repo.GetID())
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

	log.Printf("response status: %q", resp.Status)

	return nil
}

func (c *Client) RemoveRepoFromRenovatePermissions(ctx context.Context, org string, repo *github.Repository) error {
	inst, err := c.GetRenovateInstallation(ctx, org)
	if err != nil {
		return microerror.Mask(err)
	}

	path := fmt.Sprintf("/user/installations/%d/repositories/%d", inst.GetID(), repo.GetID())
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
