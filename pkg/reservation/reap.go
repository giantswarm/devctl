package reservation

import (
	"context"
	"errors"
	"fmt"
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
// does not stop the sweep from reaching the next one; its error is joined into
// the one Reap returns alongside whatever it did manage to release, so a
// caller can report every failure rather than just the first.
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
	var errs []error
	for _, cluster := range clusters {
		released, err := reapCluster(ctx, req, cluster, now)
		reaped = append(reaped, released...)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return reaped, errors.Join(errs...)
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
// push that fails on one reservation never costs the release of another. A
// reservation that fails its reason check or its release/push is joined into
// the returned error rather than stopping the rest of the cluster's sweep.
func reapCluster(ctx context.Context, req ReapRequest, cluster string, now time.Time) ([]Reaped, error) {
	reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: cluster})
	if err != nil {
		return nil, fmt.Errorf("listing reservations on cluster %q: %w", cluster, err)
	}

	var reaped []Reaped
	var errs []error
	for _, r := range reservations {
		reason, err := reapReason(ctx, req.HeadBranch, r, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("checking whether to release %q on cluster %q: %w", r.App, cluster, err))
			continue
		}
		if reason == "" {
			continue
		}

		result, condemned, condemnedReason, err := reapAndPush(ctx, ReleaseRequest{
			RepoDir: req.RepoDir,
			Cluster: cluster,
			App:     r.App,
			User:    req.User,
			// No PullRequest: the reaper releases by expiry and by rename,
			// not on behalf of a pull request.
		}, req.HeadBranch, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("releasing %q on cluster %q: %w", r.App, cluster, err))
			continue
		}
		if result.Commit == "" {
			// A rebase swapped in a fresh reservation for the same app between
			// the List above and render's own re-check: it is not the record
			// this sweep condemned, so reapAndPush left it alone.
			continue
		}

		// condemned and condemnedReason describe the record render actually
		// re-checked and deleted, which after a rebase can differ from r: r is
		// only the stale pre-push List read this sweep started from.
		reaped = append(reaped, Reaped{
			Cluster:     cluster,
			App:         result.App,
			User:        condemned.User,
			Branch:      condemned.Branch,
			PullRequest: condemned.PullRequest,
			Reason:      condemnedReason,
			Until:       condemned.Until,
			Commit:      result.Commit,
		})
	}

	return reaped, errors.Join(errs...)
}

// reapReason decides whether r must go: an expired Until is checked first, so
// a reservation already past its time never costs a GitHub call for the
// rename check. It returns "" when r is still good.
func reapReason(ctx context.Context, headBranch HeadBranchFunc, r Reservation, now time.Time) (string, error) {
	if !r.Until.After(now) {
		return ReasonExpired, nil
	}
	if r.PullRequest == "" || headBranch == nil {
		return "", nil
	}

	current, err := headBranch(ctx, r.PullRequest)
	if err != nil {
		return "", microerror.Mask(err)
	}
	if current != r.Branch {
		return ReasonRenamed, nil
	}

	return "", nil
}

// releaseAndPush releases one reservation and pushes the commit, exactly as
// the release command does for a single reservation: render (Release),
// assert, commit, then PushWithRetry rebases and re-renders should another
// release land first. Re-rendering is why req carries the caller's
// PullRequest: after a rebase the ConfigMap may hold somebody else's
// reservation for the same app, and Release must refuse that one.
func releaseAndPush(ctx context.Context, req ReleaseRequest) (ReleaseResult, error) {
	var result ReleaseResult
	render := func() error {
		var err error
		result, err = Release(req)
		return microerror.Mask(err)
	}

	if err := PushWithRetry(ctx, req.RepoDir, render); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	return result, nil
}

// reapAndPush is releaseAndPush for the reaper's own case: req carries no
// PullRequest, since Reap releases by expiry and by rename rather than on
// behalf of a pull request, so Release's own checkPullRequestHolds has
// nothing to check and cannot tell a rebase's fresh reservation for the same
// app from the stale one the sweep decided to release. Render re-lists the
// cluster and re-checks the app's own expiry and rename, with headBranch and
// now exactly as reapCluster's own reapReason call used, against the state it
// is about to write. When that re-check finds nothing left to condemn, render
// skips Release entirely and reports a zero ReleaseResult: nothing needed
// doing, and the fresh reservation is left untouched.
//
// It also returns the Reservation the re-check found and the reason it
// condemned it: after a rebase this can be a different record than the one
// the caller's req.App lookup started from, and the caller reports this one,
// not its own stale pre-push read.
func reapAndPush(ctx context.Context, req ReleaseRequest, headBranch HeadBranchFunc, now time.Time) (ReleaseResult, Reservation, string, error) {
	var result ReleaseResult
	var condemned Reservation
	var reason string
	render := func() error {
		reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: req.Cluster})
		if err != nil {
			return microerror.Mask(err)
		}
		for _, current := range reservations {
			if current.App != req.App {
				continue
			}
			r, err := reapReason(ctx, headBranch, current, now)
			if err != nil {
				return microerror.Mask(err)
			}
			if r == "" {
				result = ReleaseResult{}
				return nil
			}
			condemned, reason = current, r
			break
		}

		result, err = Release(req)
		return microerror.Mask(err)
	}

	if err := PushWithRetry(ctx, req.RepoDir, render); err != nil {
		return ReleaseResult{}, Reservation{}, "", microerror.Mask(err)
	}

	return result, condemned, reason, nil
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
