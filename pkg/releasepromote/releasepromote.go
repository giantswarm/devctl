// Package releasepromote promotes the latest release candidate of
// auto-release repositories to a stable release: what `devctl release
// promote` does. The auto-release workflow cuts a candidate vX.Y.Z-rc.N, a
// GitHub pre-release, on every releasable push; a stable release is cut
// only by running that workflow by hand with release-type stable, which
// promotes the latest candidate since the last stable release. The package
// checks that the workflow on the default branch is one that promotes,
// picks that candidate from the repository's releases reachable from the
// default branch, checks that its commit is built (the rule the workflow
// applies) and dispatches the workflow on the default branch. The workflow
// does the promotion and its own checks; nothing here waits for its run.
package releasepromote

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// Command is the envelope's command name.
const Command = "release promote"

// Workflow is the file name of the generated auto-release workflow.
const Workflow = "zz_generated.auto_release.yaml"

// workflowPath is where the workflow lives in the repository.
const workflowPath = ".github/workflows/" + Workflow

// promotionMarker is the step of the auto-release workflow that promotes the
// latest candidate on release-type stable. A workflow without it tags the
// branch head on release-type stable and checks no commit status.
const promotionMarker = "Resolve the candidate to promote"

// The workflow_dispatch input that asks the workflow for a stable release.
const (
	inputReleaseType  = "release-type"
	releaseTypeStable = "stable"
)

// The commit status state that counts as built.
const statusSuccess = "success"

// GitHub is what a promotion reads from and writes to GitHub;
// *githubclient.Client is one.
type GitHub interface {
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)
	ReadFile(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error)
	ListReleases(ctx context.Context, owner, repo string) ([]githubclient.Release, error)
	Reachable(ctx context.Context, owner, repo, ref, branch string) (bool, error)
	GetCombinedStatus(ctx context.Context, owner, repo, ref string) (githubclient.CombinedStatus, error)
	DispatchWorkflow(ctx context.Context, owner, repo, file, ref string, inputs map[string]any) error
}

// Config configures a promotion.
type Config struct {
	// GitHub is required.
	GitHub GitHub
	// DryRun checks every repository and dispatches nothing.
	DryRun bool
	// NotFoundHint is added to GitHub's 404 for a repository: what the
	// token reaches. Empty adds nothing.
	NotFoundHint string
	// Progress receives one line per repository; nil is silent.
	Progress *agentcli.Progress
}

// Promote runs the promotion of every repository, owner/repo each, in order,
// and fills result.Repositories. A repository that is not dispatched is
// reported in its entry and does not stop the others; the error is then an
// [agentcli.ExitRed] outcome naming them. Nil when every repository was
// dispatched, would be, or has nothing to promote.
func Promote(ctx context.Context, config Config, repositories []string, result *Result) error {
	if config.GitHub == nil {
		return usageErr("%T.GitHub must not be empty", config)
	}
	var refused []string
	for _, repository := range repositories {
		entry := promoteOne(ctx, config, repository)
		config.Progress.Printf("%s: %s", repository, entry.Message)
		result.Repositories = append(result.Repositories, entry)
		if !entry.OK() {
			refused = append(refused, fmt.Sprintf("%s (%s)", entry.Repository, entry.State))
		}
	}
	if len(refused) > 0 {
		return agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictRed, "%d of %d repositories not dispatched: %s", len(refused), len(repositories), strings.Join(refused, ", "))
	}
	return nil
}

func promoteOne(ctx context.Context, config Config, repository string) Repository {
	entry := Repository{Repository: repository}
	owner, repo, ok := SplitRepository(repository)
	if !ok {
		return entry.set(StateFailed, "%q is not a repository: expected owner/repo", repository)
	}
	gh := config.GitHub

	branch, err := gh.DefaultBranch(ctx, owner, repo)
	if err != nil {
		return entry.set(StateFailed, "reading the repository: %v", githubclient.ExplainNotFound(err, config.NotFoundHint))
	}
	workflow, found, err := gh.ReadFile(ctx, owner, repo, workflowPath, branch)
	if err != nil {
		return entry.set(StateFailed, "reading %s on %s: %v", workflowPath, branch, err)
	}
	if !found {
		return entry.set(StateNotAutoRelease, "no %s on %s: the repository does not release with auto-release", workflowPath, branch)
	}
	if !strings.Contains(string(workflow), promotionMarker) {
		return entry.set(StateOutdatedWorkflow, "%s on %s does not promote release candidates: its release-type stable tags the branch head; align the repository to devctl v8.102.0 or later (giantswarm/devctl#2413)", workflowPath, branch)
	}

	releases, err := gh.ListReleases(ctx, owner, repo)
	if err != nil {
		return entry.set(StateFailed, "listing the releases: %v", err)
	}
	reachable := func(tag string) (bool, error) {
		return gh.Reachable(ctx, owner, repo, tag, branch)
	}
	stable, candidate, err := SelectCandidate(releases, reachable)
	entry.Stable, entry.Candidate = stable, candidate
	if err != nil {
		return entry.set(StateFailed, "comparing the release tags with %s: %v", branch, err)
	}
	if candidate == "" {
		since := stable
		if since == "" {
			since = "inception"
		}
		return entry.set(StateNothingToPromote, "no release candidate on %s since %s", branch, since)
	}

	status, err := gh.GetCombinedStatus(ctx, owner, repo, candidate)
	if err != nil {
		return entry.set(StateFailed, "reading the commit status of %s: %v", candidate, err)
	}
	entry.StatusState = status.State
	if status.TotalCount > 0 && status.State != statusSuccess {
		return entry.set(StateNotBuilt, "the commit statuses of %s are %s: promote it once its pipelines passed", candidate, status.State)
	}

	if config.DryRun {
		return entry.set(StateWouldDispatch, "would dispatch %s on %s to promote %s", Workflow, branch, candidate)
	}
	err = gh.DispatchWorkflow(ctx, owner, repo, Workflow, branch, map[string]any{inputReleaseType: releaseTypeStable})
	if err != nil {
		return entry.set(StateFailed, "dispatching %s on %s: %v", Workflow, branch, err)
	}
	return entry.set(StateDispatched, "dispatched %s on %s to promote %s", Workflow, branch, candidate)
}

// The tags auto-release cuts: a stable vX.Y.Z and a candidate vX.Y.Z-rc.N.
var (
	stablePattern    = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	candidatePattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$`)
)

// tagVersion is a release tag with its parsed version.
type tagVersion struct {
	tag     string
	version *semver.Version
}

// SelectCandidate returns the tag of the highest stable release reachable
// from the default branch and of the highest reachable candidate newer than
// it, "" for either when there is none: the tags the workflow's `--merged
// HEAD` lets it see. reachable says whether a tag is on the default branch;
// it is asked from the highest tag down and only until the first reachable
// one, so a higher stable release or candidate of another branch (a
// backport line) is skipped without hiding the default branch's. Drafts are
// no releases; a candidate counts only as a GitHub pre-release, the form
// the workflow promotes. Versions are compared as semver, so rc.10 follows
// rc.9 and a candidate for a higher version follows one it replaced.
func SelectCandidate(releases []githubclient.Release, reachable func(tag string) (bool, error)) (stable, candidate string, err error) {
	var stables, candidates []tagVersion
	for _, rel := range releases {
		if !rel.Published {
			continue
		}
		v, err := semver.NewVersion(rel.Tag)
		if err != nil {
			continue
		}
		switch {
		case stablePattern.MatchString(rel.Tag):
			stables = append(stables, tagVersion{tag: rel.Tag, version: v})
		case candidatePattern.MatchString(rel.Tag) && rel.Prerelease:
			candidates = append(candidates, tagVersion{tag: rel.Tag, version: v})
		}
	}

	stableTag, err := highestReachable(stables, nil, reachable)
	if err != nil {
		return "", "", err
	}
	stable = stableTag.tag
	candidateTag, err := highestReachable(candidates, stableTag.version, reachable)
	if err != nil {
		return stable, "", err
	}
	return stable, candidateTag.tag, nil
}

// highestReachable is the highest of tags above floor (nil for none) that
// reachable accepts, the zero value when none does.
func highestReachable(tags []tagVersion, floor *semver.Version, reachable func(tag string) (bool, error)) (tagVersion, error) {
	slices.SortFunc(tags, func(a, b tagVersion) int { return b.version.Compare(a.version) })
	for _, t := range tags {
		if floor != nil && !t.version.GreaterThan(floor) {
			break
		}
		ok, err := reachable(t.tag)
		if err != nil {
			return tagVersion{}, fmt.Errorf("%s: %w", t.tag, err)
		}
		if ok {
			return t, nil
		}
	}
	return tagVersion{}, nil
}

// SplitRepository reads owner/repo.
func SplitRepository(s string) (owner, repo string, ok bool) {
	owner, repo, ok = strings.Cut(s, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return "", "", false
	}
	return owner, repo, true
}

func usageErr(format string, args ...any) error {
	return agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, format, args...)
}
