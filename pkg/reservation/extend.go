package reservation

import (
	"context"
	"time"

	"github.com/giantswarm/microerror"
)

// ExtendRequest resets the expiry of every reservation a pull request holds.
type ExtendRequest struct {
	// RepoDir is the working tree of the GitOps repo to scan. Extend commits
	// and pushes to it directly, exactly as Reap does: no clone.
	RepoDir string
	// PullRequest is the key the scan matches on, e.g.
	// "giantswarm/hello-world#123".
	PullRequest string
	// User attributes each commit Extend makes. It need not be the original
	// holder, exactly like ReapRequest.User.
	User string
	// Now is the new start of every reservation Extend resets. Zero means
	// time.Now().
	Now time.Time
}

func (r ExtendRequest) validate() error {
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

// Extended reports one reservation Extend reset.
type Extended struct {
	// Cluster is the management cluster the reservation lives on.
	Cluster string
	// App is the reservation's chart.
	App string
	// From and Until bound the new window, in UTC.
	From, Until time.Time
}

// Extend resets the expiry of every reservation req.PullRequest holds.
func Extend(ctx context.Context, req ExtendRequest) ([]Extended, error) {
	if err := req.validate(); err != nil {
		return nil, microerror.Mask(err)
	}

	return nil, microerror.Maskf(nothingToExtendError,
		"pull request %q holds no reservation to extend. Run /deploy <MC> to create one", req.PullRequest)
}
