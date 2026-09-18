package reservation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

// Extend resets the expiry of every reservation req.PullRequest holds across
// every enabled cluster, keeping each reservation's own stored cluster, app,
// scope and duration -- only the window moves, starting now and lasting as
// long as the existing record already did.
//
// A reservation whose Until has already passed is treated as if it did not
// exist: reviving it could silently break an exclusive lock somebody else
// legally took over the same cluster while the dead record sat unswept (see
// checkCollision). Only /deploy brings a dead reservation back.
//
// It refuses when the pull request holds no matching, unexpired record on any
// cluster, naming /deploy as the way to create one. A single reservation
// whose stored duration now exceeds its cluster's cap is refused on its own
// and does not stop the rest of the sweep, exactly as Reap behaves: Extend
// never returns a bare nil, err when some reservations did get extended.
func Extend(ctx context.Context, req ExtendRequest) ([]Extended, error) {
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

	var extended []Extended
	var errs []error
	found := false
	for _, cluster := range clusters {
		ext, matched, err := extendCluster(ctx, req, cluster, now)
		extended = append(extended, ext...)
		found = found || matched
		if err != nil {
			errs = append(errs, err)
		}
	}

	if !found {
		errs = append(errs, microerror.Maskf(nothingToExtendError,
			"pull request %q holds no reservation to extend. Run /deploy <MC> to create one", req.PullRequest))
	}

	return extended, errors.Join(errs...)
}

// extendCluster resets the expiry of every reservation on cluster that
// matches req.PullRequest and is not already expired, pushing each one
// before the next is even considered, so a push that fails on one
// reservation never costs the extension of another. matched reports whether
// any unexpired record on this cluster names the pull request at all, even
// one refused for its cap, so Extend can tell "no record" apart from "a
// record failed".
func extendCluster(ctx context.Context, req ExtendRequest, cluster string, now time.Time) (extended []Extended, matched bool, err error) {
	reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: cluster})
	if err != nil {
		return nil, false, fmt.Errorf("listing reservations on cluster %q: %w", cluster, err)
	}

	var errs []error
	for _, r := range reservations {
		if r.PullRequest != req.PullRequest || !r.Until.After(now) {
			continue
		}
		matched = true

		result, err := extendAndPush(ctx, req, cluster, r, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("extending %q on cluster %q: %w", r.App, cluster, err))
			continue
		}
		extended = append(extended, result)
	}

	return extended, matched, errors.Join(errs...)
}

// extendAndPush resets one reservation's window and pushes the commit,
// exactly as reapCluster's releaseAndPush does for a single release: render
// (which here also re-checks the cap), commit, then PushWithRetry rebases
// and re-renders should another change land first.
func extendAndPush(ctx context.Context, req ExtendRequest, cluster string, r Reservation, now time.Time) (Extended, error) {
	configMapPath := filepath.Join(req.RepoDir, clustersDir, cluster, ConfigMapFile)
	sourcePath := filepath.Join(req.RepoDir, clustersDir, cluster, collectionsDir, reservationsDir, r.App, r.App+SourceNameSuffix+".yaml")

	var result Extended
	render := func() error {
		// Read the reservation fresh from the working tree render is about to
		// write to, not from the snapshot List returned before this call: after a
		// rebase that is the post-rebase state, so a reservation that changed
		// underneath this one in the meantime is folded in instead of overwritten
		// with stale data. r.App is only an identifier -- which slot to extend --
		// and stays stable across retries.
		reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: cluster})
		if err != nil {
			return microerror.Mask(err)
		}
		var current *Reservation
		for i := range reservations {
			if reservations[i].App == r.App {
				current = &reservations[i]
				break
			}
		}
		if current == nil {
			return microerror.Maskf(notReservedError, "%s on %s no longer holds a reservation to extend", r.App, cluster)
		}
		duration := current.Until.Sub(current.From)

		maxDuration, err := clusterMaxDuration(configMapPath)
		if err != nil {
			return microerror.Mask(err)
		}
		if duration > maxDuration {
			return microerror.Maskf(invalidDurationError,
				"%s's reservation on %s lasts %s, which is now longer than the maximum of %s: release it and reserve it again",
				current.App, cluster, formatDuration(duration), formatDuration(maxDuration))
		}

		until := now.Add(duration)
		entry, err := reservationEntry(current.App, Request{User: current.User, Branch: current.Branch, PullRequest: current.PullRequest, Scope: current.Scope}, now, until)
		if err != nil {
			return microerror.Mask(err)
		}
		if err := writeReservationEntry(configMapPath, current.App, entry); err != nil {
			return microerror.Mask(err)
		}
		// The ConfigMap entry above and this are one change: a human with no
		// access to the GitOps repo reads /from and /until off the OCIRepository
		// with kubectl, and the two must never disagree, in any commit.
		if err := writeSourceAnnotations(sourcePath, now, until); err != nil {
			return microerror.Mask(err)
		}

		files := []string{
			clusterPath(cluster, ConfigMapFile),
			clusterPath(cluster, collectionsDir, reservationsDir, current.App, current.App+SourceNameSuffix+".yaml"),
		}
		if _, err := commitAll(req.RepoDir, req.User, fmt.Sprintf(
			"extend %s on %s for %s (until %s)", current.App, cluster, current.User, until.Format(time.RFC3339)), files); err != nil {
			return microerror.Mask(err)
		}

		result = Extended{Cluster: cluster, App: current.App, From: now, Until: until}
		return nil
	}

	if err := PushWithRetry(ctx, req.RepoDir, render); err != nil {
		return Extended{}, microerror.Mask(err)
	}

	return result, nil
}
