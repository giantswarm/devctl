// Package reservation implements the GitOps side of a management cluster
// reservation: it points one management cluster's copy of one collection app at
// the dev builds of one branch, as a single commit in the GitOps repo that holds
// that cluster.
package reservation

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/go-git/go-git/v5"
	gitobject "github.com/go-git/go-git/v5/plumbing/object"
)

const (
	// Namespace is the namespace every collection app object lives in.
	Namespace = "giantswarm"
	// ConfigMapFile is the per-cluster reservation state file. Its presence,
	// wired into the cluster root kustomization, is the opt-in.
	ConfigMapFile = "configmap-reservations.yaml"
	// DefaultDuration is how long a reservation lasts.
	DefaultDuration = 10 * time.Hour
	// ScopeApp is the only scope this version supports.
	ScopeApp = "app"

	clustersDir     = "management-clusters"
	collectionsDir  = "collections"
	reservationsDir = "reservations"
)

// Request is one reservation of one app on one management cluster.
type Request struct {
	// RepoDir is the working tree of the GitOps repo holding Cluster.
	RepoDir string
	// Cluster is the management cluster name, e.g. "graveler".
	Cluster string
	// App is the collection app to point at the dev builds.
	App string
	// Branch is the app repo branch whose dev builds the cluster follows.
	Branch string
	// User is the GitHub login of the holder.
	User string
	// PullRequest is the holder's pull request, e.g. "giantswarm/app#123".
	PullRequest string
	// Now is the start of the reservation. Zero means time.Now().
	Now time.Time
}

// Result reports what Reserve wrote.
type Result struct {
	// Commit is the hash of the commit Reserve made.
	Commit string
	// SourceName is the name of the new OCIRepository.
	SourceName string
	// SemverFilter is the version selector the new OCIRepository carries.
	SemverFilter string
	// Files are the repo-relative paths Reserve wrote, sorted.
	Files []string
	// From and Until bound the reservation, in UTC.
	From, Until time.Time
}

func (r Request) validate() error {
	for _, f := range []struct{ name, value string }{
		{"RepoDir", r.RepoDir},
		{"Cluster", r.Cluster},
		{"App", r.App},
		{"Branch", r.Branch},
		{"User", r.User},
	} {
		if f.value == "" {
			return microerror.Maskf(invalidConfigError, "%T.%s must not be empty", r, f.name)
		}
	}

	return nil
}

// clusterPath returns the repo-relative path of a file under the cluster
// directory.
func (r Request) clusterPath(elem ...string) string {
	return filepath.ToSlash(filepath.Join(append([]string{clustersDir, r.Cluster}, elem...)...))
}

// checkEnabled refuses a cluster the GitOps repo does not know, and a cluster
// whose owners have not opted in to reservations.
func checkEnabled(req Request) error {
	clusterDir := filepath.Join(req.RepoDir, clustersDir, req.Cluster)
	if fi, err := os.Stat(clusterDir); err != nil || !fi.IsDir() {
		return microerror.Maskf(clusterNotFoundError,
			"management cluster %q does not exist in this GitOps repo (no %s)",
			req.Cluster, req.clusterPath())
	}

	if _, err := os.Stat(filepath.Join(clusterDir, ConfigMapFile)); err != nil {
		return microerror.Maskf(clusterNotEnabledError,
			"management cluster %q is not enabled for reservations. To enable it, open a pull request on this repo that adds %s holding a ConfigMap named reservations in namespace %s with `data: {}`, and lists %s under `resources:` in %s",
			req.Cluster,
			req.clusterPath(ConfigMapFile),
			Namespace,
			ConfigMapFile,
			req.clusterPath("kustomization.yaml"))
	}

	return nil
}

// Reserve points the cluster's copy of the app at the dev builds of the branch
// and commits the change to the working tree at req.RepoDir. It renders the
// cluster's collections and refuses to commit unless the rendered output really
// carries the reservation.
func Reserve(req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := checkEnabled(req); err != nil {
		return Result{}, microerror.Mask(err)
	}

	configMapPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, ConfigMapFile)
	if err := checkNotReserved(configMapPath, req.App); err != nil {
		return Result{}, microerror.Mask(err)
	}

	from := req.Now
	if from.IsZero() {
		from = time.Now()
	}
	from = from.UTC().Truncate(time.Second)
	until := from.Add(DefaultDuration)

	collectionsPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, collectionsDir)

	before, err := renderCollections(collectionsPath)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	original, err := findSource(before, req.App)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	helmReleaseName, err := findHelmRelease(before, req.App)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	semverFilter, err := devSemverFilter(req.Branch)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	sourceName := req.App + SourceNameSuffix
	component := path.Join(reservationsDir, req.App)

	// The entry and the component are one change. Everything below is staged in
	// the working tree and lands in a single commit, or the run fails and the
	// clone is thrown away.
	err = writeComponent(filepath.Join(collectionsPath, reservationsDir, req.App),
		sourceName, helmReleaseName, devSource(original, req, semverFilter, from, until))
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := addComponent(filepath.Join(collectionsPath, "kustomization.yaml"), component); err != nil {
		return Result{}, microerror.Mask(err)
	}
	entry, err := reservationEntry(req, from, until)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := addReservationEntry(configMapPath, req.App, entry); err != nil {
		return Result{}, microerror.Mask(err)
	}

	after, err := renderCollections(collectionsPath)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := assertReserved(after, req.App, sourceName, semverFilter, original.nested("spec", "ref")); err != nil {
		return Result{}, microerror.Mask(err)
	}

	files := []string{
		req.clusterPath(ConfigMapFile),
		req.clusterPath(collectionsDir, "kustomization.yaml"),
		req.clusterPath(collectionsDir, component, "kustomization.yaml"),
		req.clusterPath(collectionsDir, component, sourceName+".yaml"),
	}
	sort.Strings(files)

	commit, err := commitAll(req, fmt.Sprintf(
		"reserve %s on %s for %s (branch %s, until %s)",
		req.App, req.Cluster, req.User, req.Branch, until.Format(time.RFC3339)))
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	return Result{
		Commit:       commit,
		SourceName:   sourceName,
		SemverFilter: semverFilter,
		Files:        files,
		From:         from,
		Until:        until,
	}, nil
}

// commitAll stages the whole working tree and commits it as the requesting user,
// so `git log` answers who reserved what without any other lookup.
func commitAll(req Request, message string) (string, error) {
	repo, err := git.PlainOpen(req.RepoDir)
	if err != nil {
		return "", microerror.Mask(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return "", microerror.Mask(err)
	}
	if err := worktree.AddGlob("."); err != nil {
		return "", microerror.Mask(err)
	}

	hash, err := worktree.Commit(message, &git.CommitOptions{
		Author: &gitobject.Signature{
			Name:  req.User,
			Email: fmt.Sprintf("%s@users.noreply.github.com", req.User),
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", microerror.Mask(err)
	}

	return hash.String(), nil
}
