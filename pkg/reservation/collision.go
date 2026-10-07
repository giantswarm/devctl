package reservation

import (
	"time"

	"github.com/giantswarm/microerror"
)

// checkCollision applies the locking rules against every active reservation on
// req.Cluster, using now as the clock: an entry whose Until is not after now
// is expired and never blocks anything, even when the reaper has not swept it
// yet.
//
// It returns the sole reservation to promote when req is an exclusive request
// that qualifies (nil otherwise), or a refusal that names the blocking
// reservation's app, holder, branch and expiry.
func checkCollision(req Request, chart string, now time.Time) (*Reservation, error) {
	all, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: req.Cluster})
	if err != nil {
		return nil, microerror.Mask(err)
	}

	var active []Reservation
	for _, r := range all {
		if r.Until.After(now) {
			active = append(active, r)
		}
	}

	if req.Scope == ScopeExclusive {
		if len(active) == 1 && active[0].User == req.User && active[0].App == chart {
			promoted := active[0]
			return &promoted, nil
		}
		if len(active) > 0 {
			return nil, refusal(clusterLockedError, active[0])
		}
		return nil, nil
	}

	for _, r := range active {
		if r.App == chart {
			return nil, refusal(alreadyReservedError, r)
		}
	}
	for _, r := range active {
		if r.Scope == ScopeExclusive {
			return nil, refusal(clusterLockedError, r)
		}
	}

	return nil, nil
}

// refusal names the holder, the app, the branch and the expiry of the
// reservation that blocks a request. A refusal reports these four things and
// nothing else changes: slice 09 puts the message straight into a
// pull-request comment.
func refusal(kind *microerror.Error, holder Reservation) error {
	return microerror.Maskf(kind,
		"%s is held by %s on branch %s until %s",
		holder.App, holder.User, holder.Branch, holder.Until.Format(time.RFC3339))
}
