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
	// DefaultDuration is how long a reservation lasts when nobody says.
	DefaultDuration = 10 * time.Hour
	// MaxDuration is the longest reservation any cluster allows. A cluster can
	// set a lower one of its own.
	MaxDuration = 7 * 24 * time.Hour
	// ScopeApp is a reservation that locks only its own app: it fails when the
	// same app already holds one, or when an exclusive reservation is active,
	// but leaves every other app on the cluster free. It is the default.
	ScopeApp = "app"
	// ScopeExclusive is a reservation that locks the whole cluster: it fails
	// against any other active reservation, unless it promotes the requester's
	// own sole one, but it still patches only the requester's own app.
	ScopeExclusive = "exclusive"

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
	// App names the collection app to point at the dev builds. Empty means the
	// chart the app repository at AppDir builds. It is required when that
	// repository holds several charts, and it overrides the chart name when a
	// chart is named differently from the chart its URL serves.
	App string
	// AppDir is a working tree of the app repository, read only to take the
	// chart name from helm/*/Chart.yaml. It is unused when App is set.
	AppDir string
	// Branch is the app repo branch whose dev builds the cluster follows.
	Branch string
	// User is the GitHub login of the holder.
	User string
	// PullRequest is the holder's pull request, e.g. "giantswarm/app#123".
	PullRequest string
	// Now is the start of the reservation. Zero means time.Now().
	Now time.Time
	// Duration is how long the reservation lasts. Zero means DefaultDuration.
	Duration time.Duration
	// Scope is the reservation's lock: ScopeApp locks only App, ScopeExclusive
	// locks the whole cluster. Empty means ScopeApp.
	Scope string
}

// Result reports what Reserve wrote.
type Result struct {
	// App is the chart Reserve resolved the request to. It is the reservation's
	// key on the cluster.
	App string
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
		{"Branch", r.Branch},
		{"User", r.User},
	} {
		if f.value == "" {
			return microerror.Maskf(invalidConfigError, "%T.%s must not be empty", r, f.name)
		}
	}

	switch r.Scope {
	case "", ScopeApp, ScopeExclusive:
	default:
		return microerror.Maskf(invalidConfigError, "%T.Scope must be %q or %q, got %q", r, ScopeApp, ScopeExclusive, r.Scope)
	}

	return nil
}

// clusterPath returns the repo-relative path of a file under the directory of
// cluster.
func clusterPath(cluster string, elem ...string) string {
	return filepath.ToSlash(filepath.Join(append([]string{clustersDir, cluster}, elem...)...))
}

// clusterPath returns the repo-relative path of a file under the cluster
// directory.
func (r Request) clusterPath(elem ...string) string {
	return clusterPath(r.Cluster, elem...)
}

// checkEnabled refuses a cluster the GitOps repo does not know, and a cluster
// whose owners have not opted in to reservations. Reserve, Release and List all
// need the same check before they touch a cluster's files.
func checkEnabled(repoDir, cluster string) error {
	clusterDir := filepath.Join(repoDir, clustersDir, cluster)
	if fi, err := os.Stat(clusterDir); err != nil || !fi.IsDir() {
		return microerror.Maskf(clusterNotFoundError,
			"management cluster %q does not exist in this GitOps repo (no %s)",
			cluster, clusterPath(cluster))
	}

	if _, err := os.Stat(filepath.Join(clusterDir, ConfigMapFile)); err != nil {
		return microerror.Maskf(clusterNotEnabledError,
			"management cluster %q is not enabled for reservations. To enable it, open a pull request on this repo that adds %s holding a ConfigMap named reservations in namespace %s with `data: {}`, and lists %s under `resources:` in %s",
			cluster,
			clusterPath(cluster, ConfigMapFile),
			Namespace,
			ConfigMapFile,
			clusterPath(cluster, "kustomization.yaml"))
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
	if err := checkEnabled(req.RepoDir, req.Cluster); err != nil {
		return Result{}, microerror.Mask(err)
	}
	if req.Scope == "" {
		req.Scope = ScopeApp
	}

	from := req.Now
	if from.IsZero() {
		from = time.Now()
	}
	from = from.UTC().Truncate(time.Second)

	configMapPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, ConfigMapFile)

	duration, err := clampedDuration(req, configMapPath)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	until := from.Add(duration)

	// The chart name, not the app repository name and not the object name, is
	// what the reservation is keyed and matched on.
	chart, err := resolveChart(req.App, req.AppDir)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	promoted, err := checkCollision(req, chart, from)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if promoted != nil {
		// A promotion keeps the reservation exactly as it was: same holder, same
		// branch, same window. Only the scope changes.
		req.Branch = promoted.Branch
		req.User = promoted.User
		req.PullRequest = promoted.PullRequest
		from = promoted.From
		until = promoted.Until
	}

	collectionsPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, collectionsDir)

	before, err := renderCollections(collectionsPath)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	resolved, err := resolveApp(before, req, chart)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	semverFilter := devSemverFilter(req.Branch)

	sourceName := chart + SourceNameSuffix
	component := path.Join(reservationsDir, chart)

	// The entry and the component are one change. Everything below is staged in
	// the working tree and lands in a single commit, or the run fails and the
	// clone is thrown away.
	err = writeComponent(filepath.Join(collectionsPath, reservationsDir, chart),
		sourceName, resolved.helmReleases, devSource(resolved.original, sourceName, req, semverFilter, from, until))
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := addComponent(filepath.Join(collectionsPath, "kustomization.yaml"), component); err != nil {
		return Result{}, microerror.Mask(err)
	}
	entry, err := reservationEntry(chart, req, from, until)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := writeReservationEntry(configMapPath, chart, entry); err != nil {
		return Result{}, microerror.Mask(err)
	}

	after, err := renderCollections(collectionsPath)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if err := assertReserved(after, resolved, sourceName, semverFilter); err != nil {
		return Result{}, microerror.Mask(err)
	}

	files := []string{
		req.clusterPath(ConfigMapFile),
		req.clusterPath(collectionsDir, "kustomization.yaml"),
		req.clusterPath(collectionsDir, component, "kustomization.yaml"),
		req.clusterPath(collectionsDir, component, sourceName+".yaml"),
	}
	sort.Strings(files)

	commit, err := commitAll(req.RepoDir, req.User, fmt.Sprintf(
		"reserve %s on %s for %s (branch %s, until %s)",
		chart, req.Cluster, req.User, req.Branch, until.Format(time.RFC3339)), files)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	return Result{
		App:          chart,
		Commit:       commit,
		SourceName:   sourceName,
		SemverFilter: semverFilter,
		Files:        files,
		From:         from,
		Until:        until,
	}, nil
}

// commitAll stages exactly files -- the repo-relative paths the caller's own
// operation wrote, added, or removed -- and commits them as user, so `git log`
// answers who reserved or released what without any other lookup. Reserve,
// Release and Extend share it: each makes exactly one commit of its own
// files, never of whatever else happens to be dirty in the working tree. A
// caller's RepoDir defaults to "." on the commands that never clone, so a
// developer running one from a GitOps checkout that also holds unrelated
// in-progress edits must never have those staged, committed or pushed here.
func commitAll(repoDir, user, message string, files []string) (string, error) {
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return "", microerror.Mask(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		return "", microerror.Mask(err)
	}
	for _, f := range files {
		if _, err := worktree.Add(f); err != nil {
			return "", microerror.Mask(err)
		}
	}

	hash, err := worktree.Commit(message, &git.CommitOptions{
		Author: &gitobject.Signature{
			Name:  user,
			Email: fmt.Sprintf("%s@users.noreply.github.com", user),
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", microerror.Mask(err)
	}

	return hash.String(), nil
}
