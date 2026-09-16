package reservation

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/giantswarm/microerror"
)

// HeadBranchFunc resolves a pull request, e.g. "giantswarm/hello-world#123",
// to its current head branch. Reap calls it only for a reservation that is
// not already expired and names a pull request.
type HeadBranchFunc func(ctx context.Context, pullRequest string) (string, error)

// ReapRequest sweeps every enabled cluster in a GitOps repo.
type ReapRequest struct {
	// RepoDir is the working tree of the GitOps repo to sweep. Reap commits and
	// pushes to it directly, exactly as Release does: no clone.
	RepoDir string
	// User attributes each release commit Reap makes, e.g. "reservation-reaper".
	// It need not be the original holder, exactly like ReleaseRequest.User.
	User string
	// Now is the clock Reap checks expiry against. Zero means time.Now().
	Now time.Time
	// HeadBranch resolves a reservation's pull request to its current head
	// branch. Required only when some active reservation names one; Reap never
	// calls it for a reservation its expiry already condemns.
	HeadBranch HeadBranchFunc
}

func (r ReapRequest) validate() error {
	switch {
	case r.RepoDir == "":
		return microerror.Maskf(invalidConfigError, "%T.RepoDir must not be empty", r)
	case r.User == "":
		return microerror.Maskf(invalidConfigError, "%T.User must not be empty", r)
	}

	return nil
}

// Reap sweeps every enabled cluster under RepoDir's management-clusters
// directory and releases every reservation that ran out of time or whose
// stored branch no longer matches its pull request's current head branch. A
// cluster that fails -- an unparsable ConfigMap, a push that never lands --
// does not stop the sweep from reaching the next one; its error comes back
// alongside whatever Reap did manage to release.
func Reap(ctx context.Context, req ReapRequest) ([]Reaped, error) {
	if err := req.validate(); err != nil {
		return nil, microerror.Mask(err)
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}

	clusters, err := enabledClusters(req.RepoDir)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var reaped []Reaped
	for _, cluster := range clusters {
		released := reapCluster(ctx, req, cluster, now)
		reaped = append(reaped, released...)
	}

	return reaped, nil
}

// enabledClusters returns the management clusters under repoDir that have
// opted in to reservations, sorted by name. A cluster the repo holds but has
// not enabled is skipped, not a failure: most of a GitOps repo's clusters
// never opt in.
func enabledClusters(repoDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(repoDir, clustersDir))
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var enabled []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := checkEnabled(repoDir, e.Name()); err != nil {
			continue
		}
		enabled = append(enabled, e.Name())
	}
	sort.Strings(enabled)

	return enabled, nil
}

// reapCluster sweeps one cluster: every reservation whose expiry passed is
// released, reported and pushed before the next one is even considered, so a
// push that fails on one reservation never costs the release of another.
func reapCluster(ctx context.Context, req ReapRequest, cluster string, now time.Time) []Reaped {
	reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: cluster})
	if err != nil {
		return nil
	}

	var reaped []Reaped
	for _, r := range reservations {
		if r.Until.After(now) {
			continue
		}

		result, err := releaseAndPush(ctx, req, cluster, r)
		if err != nil {
			continue
		}

		reaped = append(reaped, Reaped{
			Cluster:     cluster,
			App:         result.App,
			User:        r.User,
			Branch:      r.Branch,
			PullRequest: r.PullRequest,
			Reason:      ReasonExpired,
			Until:       r.Until,
			Commit:      result.Commit,
		})
	}

	return reaped
}

// releaseAndPush releases one reservation and pushes the commit, exactly as
// the release command does for a single reservation: render (Release),
// assert, commit, then PushWithRetry rebases and re-renders should another
// release land first.
func releaseAndPush(ctx context.Context, req ReapRequest, cluster string, r Reservation) (ReleaseResult, error) {
	var result ReleaseResult
	render := func() error {
		var err error
		result, err = Release(ReleaseRequest{
			RepoDir: req.RepoDir,
			Cluster: cluster,
			App:     r.App,
			User:    req.User,
		})
		return microerror.Mask(err)
	}

	if err := PushWithRetry(ctx, req.RepoDir, render); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	return result, nil
}

// Reaped reports one reservation Reap released.
type Reaped struct {
	// Cluster is the management cluster the reservation was released on.
	Cluster string
	// App is the chart Release resolved the reservation to.
	App string
	// User is the GitHub login of the reservation's original holder -- not
	// ReapRequest.User, which only attributes the release commit.
	User string
	// Branch is the reservation's stored branch: the one that makes no more
	// builds when Reason is "renamed".
	Branch string
	// PullRequest is the reservation's pull request, e.g.
	// "giantswarm/hello-world#123", or empty when it named none.
	PullRequest string
	// Reason is why Reap released it: ReasonExpired or ReasonRenamed.
	Reason string
	// Until is the reservation's expiry, in UTC.
	Until time.Time
	// Commit is the hash of the commit Release made.
	Commit string
}

const (
	// ReasonExpired means the reservation's Until had already passed.
	ReasonExpired = "expired"
	// ReasonRenamed means the reservation's stored branch no longer matches its
	// pull request's current head branch: the old branch builds nothing.
	ReasonRenamed = "renamed"
)
