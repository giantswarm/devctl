package reservation

import (
	"fmt"
	"time"

	"github.com/giantswarm/microerror"
)

// checkCollision applies the locking rules against every active reservation on
// req.Cluster, using now as the clock: an entry whose Until is not after now
// is expired and never blocks anything, even when the reaper has not swept it
// yet.
//
// It returns the sole reservation to promote when req is an exclusive request
// that qualifies (nil otherwise), the expired reservation of the same app that
// req takes over from another branch or pull request (nil otherwise), or a
// refusal that names the blocking reservation's app, holder, branch, pull
// request and expiry.
func checkCollision(req Request, chart string, now time.Time) (promoted, replaced *Reservation, err error) {
	all, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: req.Cluster})
	if err != nil {
		return nil, nil, microerror.Mask(err)
	}

	var active []Reservation
	for _, r := range all {
		if !r.Expired(now) {
			active = append(active, r)
		} else if r.App == chart && (r.Branch != req.Branch || r.PullRequest != req.PullRequest) {
			replaced = &r
		}
	}

	// The holder repeating the very same request refreshes it: it blocks nothing
	// and Reserve rewrites the entry with a fresh window.
	for _, r := range active {
		if r.App == chart && r.User == req.User && r.Branch == req.Branch &&
			r.PullRequest == req.PullRequest && r.Scope == req.Scope {
			return nil, replaced, nil
		}
	}

	if req.Scope == ScopeExclusive {
		if len(active) == 1 && active[0].User == req.User && active[0].App == chart {
			promoted := active[0]
			// A promotion keeps the existing branch and window, so a request for
			// another branch or pull request would report "Reserved" and leave
			// the cluster elsewhere: refuse it instead.
			if promoted.Branch != req.Branch || (req.PullRequest != "" && promoted.PullRequest != req.PullRequest) {
				return nil, nil, refusal(alreadyReservedError, promoted)
			}
			return &promoted, nil, nil
		}
		if len(active) > 0 {
			return nil, nil, refusal(clusterLockedError, active[0])
		}
		return nil, replaced, nil
	}

	for _, r := range active {
		if r.App == chart {
			return nil, nil, refusal(alreadyReservedError, r)
		}
	}
	for _, r := range active {
		if r.Scope == ScopeExclusive {
			return nil, nil, refusal(clusterLockedError, r)
		}
	}

	return nil, replaced, nil
}

// refusal names the holder, the app, the branch, the pull request and the
// expiry of the reservation that blocks a request, so the caller can tell who
// to ask, and says when that reservation locks the whole cluster.
func refusal(kind *microerror.Error, holder Reservation) error {
	msg := fmt.Sprintf("%s is held by %s on branch %s", holder.App, holder.User, holder.Branch)
	if holder.PullRequest != "" {
		msg += " for pull request " + holder.PullRequest
	}
	msg += " until " + holder.Until.Format(time.RFC3339)
	if holder.Scope == ScopeExclusive {
		msg += "; it is an exclusive reservation, so it locks every app on the cluster"
	}
	return microerror.Maskf(kind, "%s", msg)
}
