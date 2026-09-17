package reservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/giantswarm/microerror"
)

// ReleaseAllRequest releases every reservation one pull request holds, across
// every enabled cluster.
type ReleaseAllRequest struct {
	// RepoDir is the working tree of the GitOps repo to scan. ReleaseAll
	// commits and pushes to it directly, exactly as Reap and Extend do: no
	// clone.
	RepoDir string
	// PullRequest is the key the scan matches on, e.g.
	// "giantswarm/hello-world#123".
	PullRequest string
	// User attributes each release commit. It need not be the original holder,
	// exactly like ReapRequest.User.
	User string
}

func (r ReleaseAllRequest) validate() error {
	switch {
	case r.RepoDir == "":
		return microerror.Maskf(invalidConfigError, "%T.RepoDir must not be empty", r)
	case r.PullRequest == "":
		return microerror.Maskf(invalidConfigError, "%T.PullRequest must not be empty", r)
	case r.User == "":
		return microerror.Maskf(invalidConfigError, "%T.User must not be empty", r)
	}

	return nil
}

// Released reports one reservation ReleaseAll removed.
type Released struct {
	// Cluster is the management cluster the reservation lived on.
	Cluster string
	// App is the chart Release resolved the reservation to.
	App string
	// User is the GitHub login of the reservation's original holder -- not
	// ReleaseAllRequest.User, which only attributes the release commit.
	User string
	// Branch is the reservation's stored branch.
	Branch string
	// Commit is the hash of the commit Release made.
	Commit string
}

// ReleaseAll releases every reservation req.PullRequest holds across every
// enabled cluster. A merged or closed pull request knows neither the cluster
// nor the app it reserved, so it cannot call Release, which needs both.
func ReleaseAll(ctx context.Context, req ReleaseAllRequest) ([]Released, error) {
	if err := req.validate(); err != nil {
		return nil, microerror.Mask(err)
	}

	clusters, err := enabledClusters(req.RepoDir)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var released []Released
	var errs []error
	for _, cluster := range clusters {
		rel, err := releaseAllCluster(ctx, req, cluster)
		released = append(released, rel...)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return released, errors.Join(errs...)
}

// releaseAllCluster releases every reservation on cluster that names the pull
// request, pushing each one before the next is even considered, so a push
// that fails on one reservation never costs the release of another.
func releaseAllCluster(ctx context.Context, req ReleaseAllRequest, cluster string) ([]Released, error) {
	reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: cluster})
	if err != nil {
		return nil, fmt.Errorf("listing reservations on cluster %q: %w", cluster, err)
	}

	var released []Released
	var errs []error
	for _, r := range reservations {
		if r.PullRequest != req.PullRequest {
			continue
		}

		result, err := releaseAndPush(ctx, ReleaseRequest{
			RepoDir:     req.RepoDir,
			Cluster:     cluster,
			App:         r.App,
			User:        req.User,
			PullRequest: req.PullRequest,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("releasing %q on cluster %q: %w", r.App, cluster, err))
			continue
		}

		released = append(released, Released{
			Cluster: cluster,
			App:     result.App,
			User:    r.User,
			Branch:  r.Branch,
			Commit:  result.Commit,
		})
	}

	return released, errors.Join(errs...)
}
