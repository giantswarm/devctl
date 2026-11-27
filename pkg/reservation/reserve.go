// Package reservation implements the GitOps side of a management cluster
// reservation: it points one management cluster's app at
// the dev builds of one branch, as a single commit in the GitOps repo that holds
// that cluster.
package reservation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	// DefaultGitOpsRepository holds the configuration of every Giant Swarm
	// management cluster.
	DefaultGitOpsRepository = "giantswarm/giantswarm-management-clusters"

	clustersDir    = "management-clusters"
	collectionsDir = "collections"
)

// Request is one reservation of one app on one management cluster.
type Request struct {
	// RepoDir is the working tree of the GitOps repo holding Cluster.
	RepoDir string
	// Cluster is the management cluster name, e.g. "graveler".
	Cluster string
	// App names the chart to point at the dev builds. Empty means the
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
	// Duration is how long the reservation lasts. Zero means DefaultDuration, or
	// the cluster's maximum when that is lower.
	Duration time.Duration
	// Bundle names the bundle chart that deploys App as a member. Empty means App
	// is deployed on its own. The bundle's HelmRelease is what the reservation
	// patches, not App's source, which exists only on the cluster.
	Bundle string
	// Scope is the reservation's lock: ScopeApp locks only App, ScopeExclusive
	// locks the whole cluster. Empty means ScopeApp.
	Scope string
}

// Result reports what Reserve wrote.
type Result struct {
	// App is the chart Reserve resolved the request to. It is the reservation's
	// key on the cluster.
	App string `json:"app"`
	// PullRequest is the effective link, retained from an existing reservation
	// when an exclusive promotion omits the requested pull request.
	PullRequest string `json:"pullRequest,omitempty"`
	// Commit is the hash of the commit Reserve made.
	Commit string `json:"commit"`
	// GitOpsSource is where the reservation lives: GitOpsSourceCollections or
	// GitOpsSourceExtras.
	GitOpsSource string `json:"gitopsSource"`
	// Bundle is the bundle whose HelmRelease carries the reservation, empty for
	// a chart deployed on its own. SourceName is then the source of the bundle.
	Bundle string `json:"bundle,omitempty"`
	// SourceName is the name of every patched OCIRepository, joined with ", ".
	SourceName string `json:"sourceName"`
	// SourceURL is the OCI URL of the patched sources, empty when they do not
	// all share one, and for a bundle member, whose own source exists only on
	// the cluster. The collections' sources are not in the GitOps repo, so
	// callers cannot read it from the files.
	SourceURL string `json:"sourceURL"`
	// HelmReleases are "<namespace>/<name>" of every release the reservation
	// moves to the dev builds, sorted.
	HelmReleases []string `json:"helmReleases,omitempty"`
	// SemverFilter is the version selector the patched OCIRepository objects carry.
	SemverFilter string `json:"semverFilter"`
	// Files are the repo-relative paths Reserve wrote, sorted.
	Files []string `json:"files,omitempty"`
	// From and Until bound the reservation, in UTC.
	From  time.Time `json:"from"`
	Until time.Time `json:"until"`
	// Warnings are things the user should know that did not stop the reservation.
	Warnings []string `json:"warnings,omitempty"`
	// Replaced is the expired reservation of the same app, from another branch
	// or pull request, that Reserve took over. Its pull request still reads
	// "reserved" until somebody tells it otherwise.
	Replaced *Reservation `json:"replaced,omitempty"`
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

// checkGitOpsRepo refuses a repoDir that is not a GitOps repo checkout, the
// usual mistake when --gitops-dir points at the wrong directory. The cmd layer
// names the flag or variable that picked it.
func checkGitOpsRepo(repoDir string) error {
	if fi, err := os.Stat(filepath.Join(repoDir, clustersDir)); err == nil && fi.IsDir() {
		return nil
	}

	dir, err := filepath.Abs(repoDir)
	if err != nil {
		dir = repoDir
	}
	return microerror.Maskf(notGitOpsRepoError,
		"%s is not a checkout of the GitOps repo that holds the management cluster configuration: it has no %s directory",
		dir, clustersDir)
}

// checkEnabled refuses a cluster the GitOps repo does not know, and a cluster
// whose owners have not opted in to reservations. Reserve, Release and List all
// need the same check before they touch a cluster's files.
func checkEnabled(repoDir, cluster string) error {
	if err := checkGitOpsRepo(repoDir); err != nil {
		return err
	}

	clusterDir := filepath.Join(repoDir, clustersDir, cluster)
	if fi, err := os.Stat(clusterDir); err != nil || !fi.IsDir() {
		return microerror.Maskf(clusterNotFoundError,
			"management cluster %q does not exist in this GitOps repo (no %s). Check the --cluster name, and pull the checkout if the cluster is new",
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

// Reserve points the cluster's app at the dev builds of the branch and commits
// the change to the working tree at req.RepoDir. It renders the cluster's
// collections or extras and refuses to commit unless the rendered output really
// carries the reservation.
func Reserve(req Request) (res Result, err error) {
	if err := req.validate(); err != nil {
		return Result{}, microerror.Mask(err)
	}
	if _, err := gitIdentity(req.RepoDir); err != nil {
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

	configMapPath := configMapFilePath(req.RepoDir, req.Cluster)

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
	if err := checkNoOldReservation(req.RepoDir, req.Cluster, chart); err != nil {
		return Result{}, microerror.Mask(err)
	}

	promoted, replaced, err := checkCollision(req, chart, from)
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

	// A bundle member has no source in the GitOps repo, so what the block patches
	// is the bundle's own source and HelmRelease, looked up like an app's.
	lookup := chart
	if req.Bundle != "" {
		lookup = req.Bundle
	}

	// An app the extras serve is reserved there, by patching its own sources.
	extras, ok, err := resolveExtras(req, lookup)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	if ok {
		if err := checkBlock(req, GitOpsSourceExtras, lookup, extras); err != nil {
			return Result{}, microerror.Mask(err)
		}
		res, err := reserveInPlace(req, GitOpsSourceExtras, chart, extras, from, until, replaced)
		return res, microerror.Mask(err)
	}

	collectionsPath := filepath.Join(req.RepoDir, clustersDir, req.Cluster, collectionsDir)

	before, err := renderDir(collectionsPath)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}
	sources := collectionSources(before, lookup)
	if len(extras.sources) > 0 && len(sources) == 0 {
		return Result{}, extras.movesNothingError(req, lookup)
	}
	collections, err := resolveCollections(before, req, lookup, sources)
	if err != nil {
		return Result{}, microerror.Mask(err)
	}

	if err := checkBlock(req, GitOpsSourceCollections, lookup, collections); err != nil {
		return Result{}, microerror.Mask(err)
	}
	res, err = reserveInPlace(req, GitOpsSourceCollections, chart, collections, from, until, replaced)
	if err == nil && len(extras.sources) > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"chart %q is served from both %s and %s. Only the collections hold the reservation: the extras sources %s feed no %s",
			lookup, req.clusterPath(collectionsDir), req.clusterPath(extrasDir), strings.Join(extras.sourceNames(), ", "), helmReleaseKind))
	}

	return res, microerror.Mask(err)
}

// commitAll stages exactly files -- the repo-relative paths the caller's own
// operation wrote, added, or removed -- and commits them with the git identity
// of whoever runs devctl (user.name and user.email from the git config, never
// the reservation's --user; the commit message names the holder). Reserve,
// Release and Extend share it: each makes exactly one commit of its own
// files, never of whatever else happens to be dirty in the working tree. A
// developer's own checkout may also hold unrelated in-progress edits, which
// must never be staged, committed or pushed here.
func commitAll(repoDir, message string, files []string) (string, error) {
	author, err := gitIdentity(repoDir)
	if err != nil {
		return "", microerror.Mask(err)
	}
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

	hash, err := worktree.Commit(message, &git.CommitOptions{Author: author})
	if err != nil {
		return "", microerror.Mask(err)
	}

	return hash.String(), nil
}

// gitIdentity reads user.name and user.email through the git CLI, not go-git:
// go-git ignores include.path, where many people keep their identity. Reserve,
// Release and Extend call it before they write anything, so a missing identity
// leaves the checkout untouched.
func gitIdentity(repoDir string) (*gitobject.Signature, error) {
	get := func(key string) string {
		out, _ := exec.Command("git", "-C", repoDir, "config", "--get", key).Output() //nolint:gosec // repoDir is trusted, not user data
		return strings.TrimSpace(string(out))
	}
	name, email := get("user.name"), get("user.email")
	if name == "" || email == "" {
		return nil, microerror.Maskf(invalidConfigError,
			"git user.name and user.email are not set for %s; devctl commits with your git identity. Set them with git config", repoDir)
	}

	return &gitobject.Signature{Name: name, Email: email, When: time.Now()}, nil
}
