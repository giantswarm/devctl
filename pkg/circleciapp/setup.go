package circleciapp

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
)

// GitHub is what Setup reads on GitHub.
type GitHub interface {
	// Repository returns GitHub's id of owner/repo and its default branch.
	Repository(ctx context.Context, owner, repo string) (id int64, defaultBranch string, err error)
	// LatestRelease returns the repository's latest release, nil when it has
	// none.
	LatestRelease(ctx context.Context, owner, repo string) (*github.RepositoryRelease, error)
	// Releases returns the repository's published releases, drafts and
	// pre-releases left out.
	Releases(ctx context.Context, owner, repo string) ([]*github.RepositoryRelease, error)
	// CircleCIStatuses returns the CircleCI statuses of ref's commit, newest
	// first.
	CircleCIStatuses(ctx context.Context, owner, repo, ref string) ([]*github.RepoStatus, error)
	// HasConfig says whether ref carries .circleci/config.yml.
	HasConfig(ctx context.Context, owner, repo, ref string) (bool, error)
}

// NewGitHub is the GitHub of a go-github client.
func NewGitHub(client *github.Client) GitHub { return gitHub{client} }

type gitHub struct{ c *github.Client }

func (g gitHub) Repository(ctx context.Context, owner, repo string) (int64, string, error) {
	r, _, err := g.c.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return 0, "", err
	}
	return r.GetID(), r.GetDefaultBranch(), nil
}

func (g gitHub) LatestRelease(ctx context.Context, owner, repo string) (*github.RepositoryRelease, error) {
	release, resp, err := g.c.Repositories.GetLatestRelease(ctx, owner, repo)
	if resp != nil && resp.StatusCode == 404 {
		return nil, nil
	}
	return release, err
}

func (g gitHub) Releases(ctx context.Context, owner, repo string) ([]*github.RepositoryRelease, error) {
	var out []*github.RepositoryRelease
	opts := &github.ListOptions{PerPage: 100}
	for {
		page, resp, err := g.c.Repositories.ListReleases(ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range page {
			if !r.GetDraft() && !r.GetPrerelease() {
				out = append(out, r)
			}
		}
		if resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

func (g gitHub) HasConfig(ctx context.Context, owner, repo, ref string) (bool, error) {
	_, _, resp, err := g.c.Repositories.GetContents(ctx, owner, repo, circleciclient.ConfigFile, &github.RepositoryContentGetOptions{Ref: ref})
	if resp != nil && resp.StatusCode == 404 {
		return false, nil
	}
	return err == nil, err
}

func (g gitHub) CircleCIStatuses(ctx context.Context, owner, repo, ref string) ([]*github.RepoStatus, error) {
	return reconcile.ListCircleCIStatuses(ctx, g.c, owner, repo, ref)
}

// Options are Setup's.
type Options struct {
	// Migrate deletes the OAuth pipeline's triggers (Ensure).
	Migrate bool
	// Wait follows the runs Setup starts until they settle.
	Wait bool
	// Waiter follows them when Wait is set.
	Waiter Waiter
	// Progress receives one line per step; nil discards.
	Progress func(format string, args ...any)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Run is a pipeline Setup started, and how it ended when waited for.
type Run struct {
	Ref      circleciclient.Ref
	Pipeline circleciclient.Pipeline
	Outcome  *Outcome
}

// Report is what Setup found and did.
type Report struct {
	Result *Result
	Runs   []Run
}

// Green says every run waited for succeeded.
func (r *Report) Green() bool {
	for _, run := range r.Runs {
		if run.Outcome != nil && !run.Outcome.Green {
			return false
		}
	}
	return true
}

// oauthSince says an OAuth follow posted a status on the commit no earlier
// than the release (less the slack the engine allows): its job ran after the
// tag, which may have been the tag's.
func oauthSince(statuses []*github.RepoStatus, published time.Time) bool {
	for _, st := range statuses {
		if st.GetCreator().GetLogin() != reconcile.CircleCIAppLogin && !st.GetCreatedAt().Before(published.Add(-oauthSlack)) {
			return true
		}
	}
	return false
}

// oauthSlack matches the engine's tagBuildSlack.
const oauthSlack = 10 * time.Second

// releasesToBuild are the releases Setup checks for their tag's build: the
// latest release when it is a vX.Y.Z tag, and for a multi-image repository,
// whose latest release is a <name>/vX.Y.Z tag, the newest release of every
// image line. A latest release tagged otherwise is outside the generated tag
// filters and is not run.
func releasesToBuild(ctx context.Context, gh GitHub, owner, repo string, progress func(string, ...any)) ([]*github.RepositoryRelease, error) {
	latest, err := gh.LatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("reading the latest release of %s/%s: %w", owner, repo, err)
	}
	if latest == nil {
		return nil, nil
	}
	line, _, ok := reconcile.ReleaseLine(latest.TagName)
	switch {
	case !ok:
		progress("%s/%s: latest release %s is no vX.Y.Z tag, not run", owner, repo, latest.TagName)
		return nil, nil
	case line == "":
		return []*github.RepositoryRelease{latest}, nil
	}
	all, err := gh.Releases(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("reading the releases of %s/%s: %w", owner, repo, err)
	}
	newest := map[string]*github.RepositoryRelease{}
	versions := map[string]*semver.Version{}
	for _, r := range all {
		line, v, ok := reconcile.ReleaseLine(r.TagName)
		if !ok || line == "" {
			continue
		}
		if cur, seen := versions[line]; !seen || v.GreaterThan(cur) {
			newest[line], versions[line] = r, v
		}
	}
	lines := make([]string, 0, len(newest))
	for l := range newest {
		lines = append(lines, l)
	}
	slices.Sort(lines)
	out := make([]*github.RepositoryRelease, 0, len(lines))
	for _, l := range lines {
		out = append(out, newest[l])
	}
	return out, nil
}

// tagBuildGrace is how long after its release a tag the App's trigger saw
// may carry no status of its own build before Setup runs it: CircleCI posts
// the first status within seconds of the tag push.
const tagBuildGrace = 2 * time.Minute

// Setup sets owner/repo up on a GitHub App pipeline as the person (Ensure)
// and runs what the pipeline has not built: the release tags whose commit
// carries no status of their own build (reconcile.TagBuildStatuses on the App
// pipeline's statuses; any OAuth status after the release counts as built),
// then the default branch when Ensure wrote anything or its head carries no
// App status — a tag cut before the trigger
// existed reached no pipeline, as the scaffold's v0.1.0 of a new repository
// does. A repository without .circleci/config.yml on its default branch is
// IsNoConfig, before any write. With Wait set the runs are followed until they settle.
func Setup(ctx context.Context, ci CircleCI, gh GitHub, owner, repo string, opts Options) (*Report, error) {
	progress := opts.Progress
	if progress == nil {
		progress = func(string, ...any) {}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	id, branch, err := gh.Repository(ctx, owner, repo)
	if err != nil {
		return nil, microerror.Mask(fmt.Errorf("reading %s/%s on GitHub: %w", owner, repo, err))
	}
	config, err := gh.HasConfig(ctx, owner, repo, branch)
	if err != nil {
		return nil, microerror.Mask(fmt.Errorf("reading %s of %s/%s: %w", circleciclient.ConfigFile, owner, repo, err))
	}
	if !config {
		return nil, microerror.Maskf(noConfigError, "%s/%s carries no %s on %s: CircleCI has nothing to build", owner, repo, circleciclient.ConfigFile, branch)
	}
	repository := Repository{Owner: owner, Name: repo, ID: id}
	result, err := Ensure(ctx, ci, repository, opts.Migrate)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	report := &Report{Result: result}
	slug := repository.Slug()
	def := result.Definition
	switch {
	case result.CreatedDefinition:
		progress("%s: created the GitHub App pipeline %s (%s)", slug, def.Name, def.ID)
	default:
		progress("%s: GitHub App pipeline %s (%s) present", slug, def.Name, def.ID)
	}
	if result.CreatedTrigger {
		progress("%s: added the all-pushes trigger to %s", slug, def.Name)
	}
	switch {
	case result.EnabledSetupWorkflows:
		progress("%s: switched the project's setup workflows on", slug)
	case result.SetupWorkflowsUnread != "":
		progress("%s: could not read the project's setup workflows setting (%s); a project created since late 2023 has it on, and a pipeline that fails with \"Use of setup workflows must be enabled\" needs it switched on in the project settings", slug, result.SetupWorkflowsUnread)
	}
	for _, t := range result.DeletedOAuthTriggers {
		progress("%s: deleted the OAuth pipeline's trigger %s", slug, t)
	}
	if len(result.OAuthTriggers) > 0 {
		progress("%s: the OAuth pipeline is still triggered (%s): every push builds twice until it is gone; --migrate deletes it", slug, strings.Join(result.OAuthTriggers, ", "))
	}

	// The release tags run first: a branch run on the same commit (the
	// scaffold's, for a new repository) posts statuses that a later read
	// would otherwise take for the tag's, were the tag's run to fail.
	var refs []circleciclient.Ref
	releases, err := releasesToBuild(ctx, gh, owner, repo, progress)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	for _, release := range releases {
		published := release.GetPublishedAt().Time
		if published.IsZero() {
			published = release.GetCreatedAt().Time
		}
		statuses, err := gh.CircleCIStatuses(ctx, owner, repo, release.TagName)
		if err != nil {
			return nil, microerror.Mask(fmt.Errorf("reading the statuses of %s: %w", release.TagName, err))
		}
		// The App pipeline's statuses tell a tag build by its workflows; an
		// OAuth follow's name jobs alone, so any of them after the release
		// counts as built: a tag is never published a second time.
		built := len(reconcile.TagBuildStatuses(reconcile.AppStatuses(statuses), published)) > 0 || oauthSince(statuses, published)
		recent := now().Sub(published) < tagBuildGrace && !result.CreatedTrigger
		switch {
		case built:
			progress("%s: release %s built", slug, release.TagName)
		case recent:
			progress("%s: release %s published %s ago, its build has not reported yet", slug, release.TagName, now().Sub(published).Round(time.Second))
		default:
			refs = append(refs, circleciclient.Ref{Tag: release.TagName})
		}
	}

	// The default branch runs when the set-up changed anything, or when its
	// head carries no status of the App pipeline: a pipeline made by hand, or
	// a run of this command that failed after the set-up, leaves it unbuilt
	// until the next push.
	runBranch := result.Changed()
	if !runBranch {
		head, err := gh.CircleCIStatuses(ctx, owner, repo, branch)
		if err != nil {
			return nil, microerror.Mask(fmt.Errorf("reading the statuses of %s: %w", branch, err))
		}
		runBranch = len(reconcile.AppStatuses(head)) == 0
	}
	if runBranch {
		refs = append(refs, circleciclient.Ref{Branch: branch})
	}

	for _, ref := range refs {
		p, err := ci.RunPipeline(ctx, owner, repo, def.ID, ref)
		if err != nil {
			return nil, microerror.Mask(fmt.Errorf("running %s of %s: %w", ref, slug, err))
		}
		progress("%s: ran %s: pipeline %d, %s", slug, ref, p.Number, circleciclient.PipelineURL(owner, repo, p.Number))
		report.Runs = append(report.Runs, Run{Ref: ref, Pipeline: *p})
	}
	if !opts.Wait {
		return report, nil
	}
	for i := range report.Runs {
		run := &report.Runs[i]
		outcome, err := opts.Waiter.Wait(ctx, run.Pipeline.ID)
		if err != nil {
			return report, microerror.Mask(fmt.Errorf("waiting for pipeline %d (%s) of %s: %w", run.Pipeline.Number, run.Ref, slug, err))
		}
		run.Outcome = outcome
		switch {
		case outcome.Green && len(outcome.OnHold) > 0:
			progress("%s: pipeline %d (%s) green so far, %s on hold for an approval in CircleCI", slug, run.Pipeline.Number, run.Ref, strings.Join(outcome.OnHold, ", "))
		case outcome.Green:
			progress("%s: pipeline %d (%s) green", slug, run.Pipeline.Number, run.Ref)
		default:
			progress("%s: pipeline %d (%s) red: %s", slug, run.Pipeline.Number, run.Ref, strings.Join(outcome.Failed, ", "))
		}

	}
	return report, nil
}
