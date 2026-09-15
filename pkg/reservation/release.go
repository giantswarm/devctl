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

	collectionsPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, collectionsDir)
	component := path.Join(reservationsDir, chart)
	sourceName := chart + SourceNameSuffix

	// The reverse of Reserve's writes, in the same single working-tree commit:
	// the component directory Reserve created, the line in collections'
	// kustomization.yaml that references it, and the ConfigMap entry.
	if err := os.RemoveAll(filepath.Join(collectionsPath, reservationsDir, chart)); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := removeComponent(filepath.Join(collectionsPath, "kustomization.yaml"), component); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}
	if err := removeReservationEntry(configMapPath, chart); err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	files := []string{
		clusterPath(req.Cluster, ConfigMapFile),
		clusterPath(req.Cluster, collectionsDir, "kustomization.yaml"),
		clusterPath(req.Cluster, collectionsDir, component, "kustomization.yaml"),
		clusterPath(req.Cluster, collectionsDir, component, sourceName+".yaml"),
	}
	sort.Strings(files)

	commit, err := commitAll(req.RepoDir, req.User, fmt.Sprintf("release %s on %s (by %s)", chart, req.Cluster, req.User))
	if err != nil {
		return ReleaseResult{}, microerror.Mask(err)
	}

	return ReleaseResult{App: chart, Commit: commit, Files: files}, nil
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
