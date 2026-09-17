package reservation

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

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
	// request's reservation is refused, so /undeploy <MC> cannot free a
	// cluster somebody else is testing on. Empty means no check: that is the
	// force-release a human runs from a laptop to clear a stuck lock, and it
	// is what Reap uses.
	PullRequest string
}

// ReleaseResult reports what Release removed.
type ReleaseResult struct {
	// App is the chart Release resolved the request to.
	App string
	// Commit is the hash of the commit Release made.
	Commit string
	// Files are the repo-relative paths Release removed or edited, sorted.
	Files []string
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

// Release undoes exactly what Reserve wrote for one app on one cluster:
// deleting the reservation's Kustomize component, the one line that references
// it, and the reservation's entry in the cluster's ConfigMap. It restores the
// working tree byte for byte to the state a matching Reserve found it in, and
// commits the change. It works on a working tree only: like Reserve, it never
// clones and never pushes.
func Release(req ReleaseRequest) (ReleaseResult, error) {
	if err := req.validate(); err != nil {
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

	collectionsPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, collectionsDir)
	component := path.Join(reservationsDir, chart)
	sourceName := chart + SourceNameSuffix

	// The reverse of Reserve's writes, in the same single working-tree commit:
	// the line in collections' kustomization.yaml that references the
	// component, the component directory Reserve created, and the ConfigMap
	// entry. The reference is removed first: removeComponent now refuses when
	// it cannot find what it is asked to remove, so a components list a human
	// reformatted since Reserve wrote it fails here, before anything is
	// deleted, rather than after the directory is already gone and the stale
	// reference is the only thing left pointing at it.
	if err := removeComponent(filepath.Join(collectionsPath, "kustomization.yaml"), component); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := os.RemoveAll(filepath.Join(collectionsPath, reservationsDir, chart)); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := removeReservationEntry(configMapPath, chart); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	// The only trustworthy check that the release actually took effect: render
	// the collections the way kustomize-controller does and confirm the
	// reservation's source, and every instance patched to it, are really gone.
	after, err := renderCollections(collectionsPath)
	if err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := assertReleased(after, sourceName); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	files := []string{
		clusterPath(req.Cluster, ConfigMapFile),
		clusterPath(req.Cluster, collectionsDir, "kustomization.yaml"),
		clusterPath(req.Cluster, collectionsDir, component, "kustomization.yaml"),
		clusterPath(req.Cluster, collectionsDir, component, sourceName+".yaml"),
	}
	sort.Strings(files)

	commit, err := commitAll(req.RepoDir, req.User, fmt.Sprintf("release %s on %s (by %s)", chart, req.Cluster, req.User), files)
	if err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	return ReleaseResult{App: chart, Commit: commit, Files: files}, nil
}

// assertReleased refuses a render that still carries the reservation after
// the component reference, the component directory, and the ConfigMap entry
// have all been removed: the last check before the commit, exactly as
// Reserve's assertReserved is the last check before its own. A render that
// only succeeds proves nothing on its own: it just means removeComponent's
// matcher happened to find and remove a line, not that the release actually
// took effect on the cluster.
func assertReleased(objects []object, sourceName string) error {
	if _, ok := findSource(objects, sourceName); ok {
		return microerror.Maskf(renderAssertionError,
			"the rendered collections still carry a %s named %q after release", ociRepositoryKind, sourceName)
	}
	for _, o := range objects {
		if o.kind() != helmReleaseKind || o.namespace() != Namespace {
			continue
		}
		if name, _ := o.nested("spec", "chartRef", "name").(string); name == sourceName {
			return microerror.Maskf(renderAssertionError,
				"%s %q still takes its chart from %q after release", helmReleaseKind, o.name(), sourceName)
		}
	}

	return nil
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
// holds. Without it, /undeploy <MC> run from any pull request in the repo
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
