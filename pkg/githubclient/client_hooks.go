package githubclient

import (
	"context"
	"errors"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
)

// hookDeliveriesPage bounds ListHookDeliveries: the delivery of a fresh push
// is among the newest.
const hookDeliveriesPage = 100

// ListHooks returns the webhooks of owner/repo. GitHub answers 403 to an
// identity without the repository permission Webhooks (read), [IsForbidden].
func (c *Client) ListHooks(ctx context.Context, owner, repo string) ([]*github.Hook, error) {
	hooks, _, err := c.ghClient.Repositories.ListHooks(ctx, owner, repo, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return hooks, nil
}

// ListHookDeliveries returns the newest deliveries of the webhook hookID of
// owner/repo, newest first, without their payloads: one page of at most
// [hookDeliveriesPage].
func (c *Client) ListHookDeliveries(ctx context.Context, owner, repo string, hookID int64) ([]*github.HookDelivery, error) {
	deliveries, _, err := c.ghClient.Repositories.ListHookDeliveries(ctx, owner, repo, hookID, &github.ListCursorOptions{PerPage: hookDeliveriesPage})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return deliveries, nil
}

// HookDelivery returns one delivery of the webhook hookID with its request
// payload, which the listing leaves out.
func (c *Client) HookDelivery(ctx context.Context, owner, repo string, hookID, deliveryID int64) (*github.HookDelivery, error) {
	delivery, _, err := c.ghClient.Repositories.GetHookDelivery(ctx, owner, repo, hookID, deliveryID)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	return delivery, nil
}

// RedeliverHookDelivery asks GitHub to send the delivery deliveryID of the
// webhook hookID again (POST …/deliveries/{id}/attempts). GitHub answers 202,
// the redelivery queued, which go-github reports as an [github.AcceptedError]
// and this method as success; it answers 403 to an identity without the
// repository permission Webhooks (read and write), [IsForbidden].
func (c *Client) RedeliverHookDelivery(ctx context.Context, owner, repo string, hookID, deliveryID int64) error {
	_, _, err := c.ghClient.Repositories.RedeliverHookDelivery(ctx, owner, repo, hookID, deliveryID)
	var accepted *github.AcceptedError
	if errors.As(err, &accepted) {
		return nil
	}
	if err != nil {
		return microerror.Mask(err)
	}
	return nil
}
