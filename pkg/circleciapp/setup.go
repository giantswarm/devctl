package circleciapp

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// tagBuildGrace is how long after its release a tag the App's trigger saw
// may carry no status of its own build before Setup runs it: CircleCI posts
// the first status within seconds of the tag push.
const tagBuildGrace = 2 * time.Minute

// Setup sets owner/repo up on a GitHub App pipeline as the person (Ensure)
// and runs what the pipeline has not built: the default branch when Ensure
// wrote anything, and the latest release when its tag carries no status of
// its own build (reconcile.TagBuildStatuses) — a tag cut before the trigger
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
	for _, t := range result.DeletedOAuthTriggers {
		progress("%s: deleted the OAuth pipeline's trigger %s", slug, t)
	}
	if len(result.OAuthTriggers) > 0 {
		progress("%s: the OAuth pipeline is still triggered (%s): every push builds twice until it is gone; --migrate deletes it", slug, strings.Join(result.OAuthTriggers, ", "))
	}

	var refs []circleciclient.Ref
	if result.Changed() {
		refs = append(refs, circleciclient.Ref{Branch: branch})
	}
	release, err := gh.LatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, microerror.Mask(fmt.Errorf("reading the latest release of %s: %w", slug, err))
	}
	if release != nil {
		published := release.GetPublishedAt().Time
		if published.IsZero() {
			published = release.GetCreatedAt().Time
		}
		statuses, err := gh.CircleCIStatuses(ctx, owner, repo, release.GetTagName())
		if err != nil {
			return nil, microerror.Mask(fmt.Errorf("reading the statuses of %s: %w", release.GetTagName(), err))
		}
		built := len(reconcile.TagBuildStatuses(statuses, published)) > 0
		recent := now().Sub(published) < tagBuildGrace && !result.CreatedTrigger
		switch {
		case built:
			progress("%s: release %s built", slug, release.GetTagName())
		case recent:
			progress("%s: release %s published %s ago, its build has not reported yet", slug, release.GetTagName(), now().Sub(published).Round(time.Second))
		default:
			refs = append(refs, circleciclient.Ref{Tag: release.GetTagName()})
		}
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
		if outcome.Green {
			progress("%s: pipeline %d (%s) green", slug, run.Pipeline.Number, run.Ref)
		} else {
			progress("%s: pipeline %d (%s) red: %s", slug, run.Pipeline.Number, run.Ref, strings.Join(outcome.Failed, ", "))
		}
	}
	return report, nil
}
