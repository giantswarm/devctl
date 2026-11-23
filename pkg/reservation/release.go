package reservation

import (
	"path/filepath"

	"github.com/giantswarm/microerror"
)

// ReleaseRequest identifies the reservation to release.
type ReleaseRequest struct {
	// RepoDir is the working tree of the GitOps repo holding Cluster.
	RepoDir string
	// Cluster is the management cluster name, e.g. "graveler".
	Cluster string
	// App names the chart to release. Empty means the chart the app repository
	// at AppDir builds. Release() keys the reservation on the resolved chart,
	// exactly as Reserve does, so this can be Result.App from the original
	// Reserve call.
	App string
	// AppDir is a working tree of the app repository, read only to take the
	// chart name from helm/*/Chart.yaml. It is unused when App is set.
	AppDir string
	// User is the GitHub login of whoever releases the reservation. It need not
	// be the original holder: a colleague or an on-call engineer can release for
	// someone else.
	User string
	// PullRequest, when set, restricts the release to a reservation that pull
	// request holds, e.g. "giantswarm/hello-world#123". Any other pull
	// request's reservation is refused, so releasing from another pull request cannot free a
	// cluster somebody else is testing on. Empty means no check: that is the
	// force-release a human runs from a laptop to clear a stuck lock, and it
	// is what Reap uses.
	PullRequest string
	// DryRun is read only by the sweeps that push: Release itself never
	// pushes.
	DryRun bool
}

// ReleaseResult reports what Release removed.
type ReleaseResult struct {
	// App is the chart Release resolved the request to.
	App string `json:"app"`
	// Commit is the hash of the commit Release made.
	Commit string `json:"commit"`
	// Files are the repo-relative paths Release removed or edited, sorted.
	Files []string `json:"files,omitempty"`
	// GitOpsSource is where the reservation lived, GitOpsSourceCollections or
	// GitOpsSourceExtras.
	GitOpsSource string `json:"gitopsSource"`
}

func (r ReleaseRequest) validate() error {
	switch {
	case r.RepoDir == "":
		return microerror.Maskf(invalidConfigError, "%T.RepoDir must not be empty", r)
	case r.Cluster == "":
		return microerror.Maskf(invalidConfigError, "%T.Cluster must not be empty", r)
	case r.User == "":
		return microerror.Maskf(invalidConfigError, "%T.User must not be empty", r)
	}

	return nil
}

// Release undoes exactly what Reserve wrote for one app on one cluster: the
// reservation block in the kustomization of its GitOps source, and the
// reservation's entry in the cluster's ConfigMap. It restores the
// working tree byte for byte to the state a matching Reserve found it in, and
// commits the change. It works on a working tree only: like Reserve, it never
// clones and never pushes.
func Release(req ReleaseRequest) (res ReleaseResult, err error) {
	if err := req.validate(); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if _, err := gitIdentity(req.RepoDir); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := checkEnabled(req.RepoDir, req.Cluster); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	chart, err := resolveChart(req.App, req.AppDir)
	if err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	configMapPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, ConfigMapFile)
	if err := checkReserved(configMapPath, chart); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := checkPullRequestHolds(req, chart); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	res, err = releaseInPlace(req, gitopsSourceOf(req.RepoDir, req.Cluster, chart), chart)

	return res, microerror.Mask(err)
}

// checkReserved refuses an app that holds no reservation on the cluster: there
// is nothing for Release to undo.
func checkReserved(path, app string) error {
	lines, err := readLines(path)
	if err != nil {
		return microerror.Mask(err)
	}
	if _, ok := findEntry(lines, app); !ok {
		return microerror.Maskf(notReservedError,
			"%q holds no reservation on this cluster", app)
	}

	return nil
}

// checkPullRequestHolds refuses to release a reservation another pull request
// holds. Without it, a release by any pull request in the repo
// frees a cluster somebody else is testing on: Release keys a reservation on
// the cluster and the chart alone, and every pull request in an app repo
// resolves to the same chart.
//
// An empty req.PullRequest skips the check. That is the force-release a human
// runs from a laptop to clear a stuck lock, and it is the form Reap uses.
func checkPullRequestHolds(req ReleaseRequest, chart string) error {
	if req.PullRequest == "" {
		return nil
	}

	reservations, err := List(ListRequest{RepoDir: req.RepoDir, Cluster: req.Cluster})
	if err != nil {
		return microerror.Mask(err)
	}
	for _, r := range reservations {
		if r.App != chart {
			continue
		}
		if r.PullRequest != req.PullRequest {
			return microerror.Maskf(notReservedError,
				"%q on %s is reserved by pull request %q, not by %q: release it from the pull request that holds it",
				chart, req.Cluster, r.PullRequest, req.PullRequest)
		}

		return nil
	}

	return nil
}
