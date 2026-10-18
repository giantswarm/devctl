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
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
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

// versionMarker is the promotion step reading the workflow_dispatch input
// version: a workflow without it promotes only the latest candidate.
const versionMarker = "inputs.version"

// The workflow_dispatch input that asks the workflow for a stable release.
const (
	inputReleaseType  = "release-type"
	releaseTypeStable = "stable"
)

// inputVersion is the workflow_dispatch input naming the candidate to
// promote instead of the latest.
const inputVersion = "version"

// The commit status state that counts as built, and the failed ones.
const (
	statusSuccess = "success"
	statusFailure = "failure"
	statusError   = "error"
)

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
	// Version is the candidate vX.Y.Z-rc.N to promote instead of the
	// latest; empty promotes the latest. [ValidVersion] checks its form.
	Version string
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
		for _, warning := range entry.Warnings {
			config.Progress.Printf("%s: warning: %s", repository, warning)
		}
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

	if config.Version != "" && !strings.Contains(string(workflow), versionMarker) {
		return entry.set(StateOutdatedWorkflow, "%s on %s promotes only the latest release candidate, not one named with --version: align the repository to the current devctl (giantswarm/devctl#2550)", workflowPath, branch)
	}

	releases, err := gh.ListReleases(ctx, owner, repo)
	if err != nil {
		return entry.set(StateFailed, "listing the releases: %v", err)
	}
	reachable := func(tag string) (bool, error) {
		return gh.Reachable(ctx, owner, repo, tag, branch)
	}
	var stable, candidate string
	if config.Version != "" {
		stable, candidate, err = SelectVersion(releases, reachable, config.Version)
	} else {
		stable, candidate, err = SelectCandidate(releases, reachable)
	}
	entry.Stable, entry.Candidate = stable, candidate
	switch {
	case errors.Is(err, ErrNotPrerelease) && config.Version != "":
		return entry.set(StateFailed, "%s is a full GitHub release, not a pre-release: the workflow refuses to promote it", candidate)
	case errors.Is(err, ErrNotPrerelease):
		return entry.set(StateFailed, "the highest release candidate on %s, %s, is a full GitHub release, not a pre-release: the workflow refuses to promote it", branch, candidate)
	case errors.Is(err, ErrNotCandidate):
		return entry.set(StateFailed, "%s is not a release candidate to promote on %s: %v", config.Version, branch, err)
	case err != nil:
		return entry.set(StateFailed, "comparing the release tags with %s: %v", branch, err)
	}
	if candidate == "" {
		return entry.set(StateNothingToPromote, "no release candidate on %s since %s", branch, sinceOrInception(stable))
	}

	status, err := gh.GetCombinedStatus(ctx, owner, repo, candidate)
	if err != nil {
		return entry.set(StateFailed, "reading the commit status of %s: %v", candidate, err)
	}
	entry.StatusState = status.State
	built, advisory := builtState(status)
	if built != statusSuccess {
		return notBuilt(ctx, gh, owner, repo, entry, releases, reachable, built)
	}
	for _, s := range advisory {
		entry.Warnings = append(entry.Warnings, fmt.Sprintf("the commit status %s of %s is %s; the release does not require it, so the candidate is judged without it", s.Context, candidate, s.State))
	}

	inputs := map[string]any{inputReleaseType: releaseTypeStable}
	leftOut := ""
	if config.Version != "" {
		inputs[inputVersion] = candidate
		entry.Later, err = LaterCandidates(releases, reachable, candidate)
		if err != nil {
			entry.Warnings = append(entry.Warnings, fmt.Sprintf("listing the release candidates cut after %s failed: %v; the stable release is %s's commit whatever came after it", candidate, err, candidate))
		}
		if len(entry.Later) > 0 {
			leftOut = fmt.Sprintf("; not in it, cut after it: %s", strings.Join(entry.Later, ", "))
		}
	}
	if config.DryRun {
		return entry.set(StateWouldDispatch, "would dispatch %s on %s to promote %s%s", Workflow, branch, candidate, leftOut)
	}
	err = gh.DispatchWorkflow(ctx, owner, repo, Workflow, branch, inputs)
	if githubclient.IsForbidden(err) {
		return entry.set(StateFailed, "dispatching %s on %s: %v; the token needs Actions write on %s", Workflow, branch, err, repository)
	}
	if err != nil {
		return entry.set(StateFailed, "dispatching %s on %s: %v", Workflow, branch, err)
	}
	return entry.set(StateDispatched, "dispatched %s on %s to promote %s%s", Workflow, branch, candidate, leftOut)
}

// notBuilt is entry refused as not_built, its candidate's statuses judged
// built (the state builtState returned, not success), naming the newest other candidate that is built: the one the caller
// can promote with --version instead of waiting. A read failing while
// looking for it leaves it out and says so; the refusal stands either way.
func notBuilt(ctx context.Context, gh GitHub, owner, repo string, entry Repository, releases []githubclient.Release, reachable func(tag string) (bool, error), built string) Repository {
	refusal := fmt.Sprintf("the commit statuses of %s are %s", entry.Candidate, built)
	newest, err := NewestBuilt(releases, reachable, func(tag string) (bool, error) {
		status, err := gh.GetCombinedStatus(ctx, owner, repo, tag)
		if err != nil {
			return false, err
		}
		state, _ := builtState(status)
		return state == statusSuccess, nil
	}, entry.Candidate)
	entry.NewestBuilt = newest
	switch {
	case err != nil:
		return entry.set(StateNotBuilt, "%s: promote it once its pipelines passed; looking for a built candidate failed: %v", refusal, err)
	case newest == "":
		return entry.set(StateNotBuilt, "%s: promote it once its pipelines passed; no other release candidate since %s is built", refusal, sinceOrInception(entry.Stable))
	}
	return entry.set(StateNotBuilt, "%s: promote it once its pipelines passed, or promote the newest built candidate with --version %s", refusal, newest)
}

func sinceOrInception(stable string) string {
	if stable == "" {
		return "inception"
	}
	return stable
}

// builtState is the candidate's combined commit status as the promotion
// judges it, success when it is built, and the failed statuses of advisory
// jobs ([circleciclient.AdvisoryStatus]) it was judged without: the rule
// devctl release wait applies. A commit no context reports on is built. A
// failure that is not an advisory job's keeps GitHub's combined state, and
// a context still running beside an advisory failure is pending.
func builtState(status githubclient.CombinedStatus) (string, []githubclient.CommitStatus) {
	if status.TotalCount == 0 || status.State == statusSuccess {
		return statusSuccess, nil
	}
	state := statusSuccess
	var advisory []githubclient.CommitStatus
	for _, s := range status.Statuses {
		switch s.State {
		case statusSuccess:
		case statusFailure, statusError:
			if !circleciclient.AdvisoryStatus(s.Context) {
				return status.State, nil
			}
			advisory = append(advisory, s)
		default:
			state = s.State
		}
	}
	if len(advisory) == 0 {
		// GitHub's state is not explained by the contexts read.
		return status.State, nil
	}
	return state, advisory
}

// The tags auto-release cuts: a stable vX.Y.Z and a candidate vX.Y.Z-rc.N.
var (
	stablePattern    = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	candidatePattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$`)
)

// ErrNotPrerelease is the highest candidate being a full GitHub release, not
// a pre-release: the workflow stops on it rather than promote a lower one.
var ErrNotPrerelease = errors.New("not a GitHub pre-release")

// tagVersion is a release tag with its parsed version.
type tagVersion struct {
	tag        string
	version    *semver.Version
	prerelease bool
}

// SelectCandidate returns the tag of the highest stable release reachable
// from the default branch and of the highest reachable candidate newer than
// it, "" for either when there is none: the tags the workflow's `--merged
// HEAD` lets it see. reachable says whether a tag is on the default branch;
// it is asked from the highest tag down and only until the first reachable
// one, so a higher stable release or candidate of another branch (a
// backport line) is skipped without hiding the default branch's. Drafts are
// no releases. The highest candidate must be a GitHub pre-release, the form
// the workflow promotes: a full release there is [ErrNotPrerelease] with its
// tag, never a lower candidate, as the workflow refuses it too. Versions are
// compared as semver, so rc.10 follows rc.9 and a candidate for a higher
// version follows one it replaced.
func SelectCandidate(releases []githubclient.Release, reachable func(tag string) (bool, error)) (stable, candidate string, err error) {
	stables, candidates := parseReleases(releases)
	stableTag, err := highestReachable(stables, nil, reachable)
	if err != nil {
		return "", "", err
	}
	stable = stableTag.tag
	candidateTag, err := highestReachable(candidates, stableTag.version, reachable)
	if err != nil {
		return stable, "", err
	}
	if candidateTag.tag != "" && !candidateTag.prerelease {
		return stable, candidateTag.tag, fmt.Errorf("%s: %w", candidateTag.tag, ErrNotPrerelease)
	}
	return stable, candidateTag.tag, nil
}

// ErrNotCandidate is a version named for promotion that is not a release
// candidate the workflow promotes: no published release of that tag, not
// newer than the last stable release, or not on the default branch.
var ErrNotCandidate = errors.New("not a release candidate to promote")

// notCandidate is [ErrNotCandidate] saying why alone.
type notCandidate string

func (e notCandidate) Error() string        { return string(e) }
func (e notCandidate) Is(target error) bool { return target == ErrNotCandidate }

// ValidVersion says whether version has the form of a candidate tag,
// vX.Y.Z-rc.N.
func ValidVersion(version string) bool {
	return candidatePattern.MatchString(version)
}

// SelectVersion is [SelectCandidate] for the named candidate version instead
// of the highest: the tag of the highest stable release reachable from the
// default branch, and version once it is a published release newer than
// that stable release and reachable, the candidates the workflow's `version`
// input accepts. Anything else is [ErrNotCandidate] saying why; a candidate
// that is a full release is [ErrNotPrerelease], as the workflow refuses it.
func SelectVersion(releases []githubclient.Release, reachable func(tag string) (bool, error), version string) (stable, candidate string, err error) {
	stables, candidates := parseReleases(releases)
	stableTag, err := highestReachable(stables, nil, reachable)
	if err != nil {
		return "", "", err
	}
	stable = stableTag.tag
	i := slices.IndexFunc(candidates, func(t tagVersion) bool { return t.tag == version })
	if i < 0 {
		return stable, "", notCandidate("no published GitHub release " + version)
	}
	target := candidates[i]
	if stableTag.version != nil && !target.version.GreaterThan(stableTag.version) {
		return stable, "", notCandidate("not newer than the last stable release " + stable)
	}
	ok, err := reachable(target.tag)
	if err != nil {
		return stable, "", fmt.Errorf("%s: %w", target.tag, err)
	}
	if !ok {
		return stable, "", notCandidate("not on the default branch")
	}
	if !target.prerelease {
		return stable, target.tag, fmt.Errorf("%s: %w", target.tag, ErrNotPrerelease)
	}
	return stable, target.tag, nil
}

// NewestBuilt is the highest release candidate other than except that the
// workflow would promote with `version` and that built accepts, "" when there
// is none: a published pre-release newer than the highest stable release
// reachable from the default branch, and reachable itself. Candidates are
// asked highest first, built only for reachable pre-releases, and only until
// the first built one.
func NewestBuilt(releases []githubclient.Release, reachable, built func(tag string) (bool, error), except string) (string, error) {
	stables, candidates := parseReleases(releases)
	stableTag, err := highestReachable(stables, nil, reachable)
	if err != nil {
		return "", err
	}
	slices.SortFunc(candidates, func(a, b tagVersion) int { return b.version.Compare(a.version) })
	for _, t := range candidates {
		if stableTag.version != nil && !t.version.GreaterThan(stableTag.version) {
			break
		}
		if t.tag == except || !t.prerelease {
			continue
		}
		ok, err := reachable(t.tag)
		if err != nil {
			return "", fmt.Errorf("%s: %w", t.tag, err)
		}
		if !ok {
			continue
		}
		ok, err = built(t.tag)
		if err != nil {
			return "", fmt.Errorf("%s: %w", t.tag, err)
		}
		if ok {
			return t.tag, nil
		}
	}
	return "", nil
}

// LaterCandidates are the release candidates higher than candidate that are
// reachable from the default branch, lowest first: those a promotion of
// candidate leaves out, since the workflow tags candidate's commit. Nil when
// there is none.
func LaterCandidates(releases []githubclient.Release, reachable func(tag string) (bool, error), candidate string) ([]string, error) {
	pinned, err := semver.NewVersion(candidate)
	if err != nil {
		return nil, err
	}
	_, candidates := parseReleases(releases)
	slices.SortFunc(candidates, func(a, b tagVersion) int { return a.version.Compare(b.version) })
	var later []string
	for _, t := range candidates {
		if !t.version.GreaterThan(pinned) {
			continue
		}
		ok, err := reachable(t.tag)
		if err != nil {
			return later, fmt.Errorf("%s: %w", t.tag, err)
		}
		if ok {
			later = append(later, t.tag)
		}
	}
	return later, nil
}

// parseReleases sorts the published releases into stable releases and
// candidates by their tags; other tags are no releases auto-release cuts.
func parseReleases(releases []githubclient.Release) (stables, candidates []tagVersion) {
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
		case candidatePattern.MatchString(rel.Tag):
			candidates = append(candidates, tagVersion{tag: rel.Tag, version: v, prerelease: rel.Prerelease})
		}
	}
	return stables, candidates
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
