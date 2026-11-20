package circleciapp

import (
	"context"
	"fmt"
	"maps"
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
	// CommitSHA returns the commit ref points at.
	CommitSHA(ctx context.Context, owner, repo, ref string) (string, error)
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

func (g gitHub) CommitSHA(ctx context.Context, owner, repo, ref string) (string, error) {
	sha, _, err := g.c.Repositories.GetCommitSHA1(ctx, owner, repo, ref, "")
	return sha, err
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
	// CancelledFollowWorkflows names the workflows of the follow's OAuth
	// build of the default branch that Setup cancelled, when Ensure followed
	// the project.
	CancelledFollowWorkflows []string
	// FollowBuildUncancelled is why that build could not be cancelled,
	// empty when it was or nothing of it ran: the branch run still follows.
	FollowBuildUncancelled string
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

// Setup sets owner/repo up on a GitHub App pipeline as the person (Ensure)
// and runs what the pipeline has not built. First the release tags whose
// commit carries no status of their own build (reconcile.TagBuildStatuses on
// the App pipeline's statuses for single-image tags; multi-image tags are
// checked against each workflow's checkout tag). An OAuth status after a
// single-image release counts as built; ambiguous OAuth evidence for an
// image tag stops dispatch. A tag cut before the trigger existed reached no
// pipeline, as the scaffold's v0.1.0 of a new repository does. Then the default branch, when
// Ensure wrote anything or its head carries no App status, unless a tag run
// builds the branch's head already. After a follow that created the project,
// the running workflows of the follow's OAuth build of the default branch are
// cancelled first (cancelFollowBuild). A repository without
// .circleci/config.yml on its default branch is IsNoConfig, before any
// write. With Wait set the runs are followed until they settle.
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
	if result.FollowedProject {
		progress("%s: CircleCI knew no project, followed it as you to create it", slug)
	}
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

	// The release tags run first, the default branch after them, and not at
	// all when a tag run builds the branch's head already.
	var refs []circleciclient.Ref
	releases, err := releasesToBuild(ctx, gh, owner, repo, progress)
	if err != nil {
		return nil, microerror.Mask(err)
	}
	builds := tagBuildReader{
		ci: ci, project: circleciclient.ProjectSlug(owner, repo),
		workflows: map[string]string{}, pipelines: map[string]string{},
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
		// An OAuth status names its job, whose pipeline names the ref it
		// checked out: a tag an OAuth pipeline built is never republished,
		// and the default branch build of the follow that created the
		// project, on the commit a first release shares, is no build of
		// the tag.
		appBuilt := len(reconcile.TagBuildStatuses(reconcile.AppStatuses(statuses), published)) > 0
		line, _, _ := reconcile.ReleaseLine(release.TagName)
		if line != "" {
			// Image tags can share a commit and its status contexts. Only
			// the checkout tag of a workflow's pipeline identifies its image.
			appBuilt, err = builds.built(ctx, release.TagName, reconcile.AppStatuses(statuses))
			if err != nil {
				return nil, microerror.Mask(fmt.Errorf("checking the build of %s: %w", release.TagName, err))
			}
		}
		built := appBuilt
		if !built {
			oauthBuilt, identified, err := builds.oauthBuilt(ctx, owner, repo, release.TagName, statuses, published)
			if err != nil {
				return nil, microerror.Mask(fmt.Errorf("checking the OAuth build of %s: %w", release.TagName, err))
			}
			if line != "" && oauthBuilt && !identified {
				return nil, fmt.Errorf("cannot identify which image tag the OAuth statuses built; refusing to republish %s", release.TagName)
			}
			built = oauthBuilt
		}
		recent := now().Sub(published) < reconcile.TagBuildGrace && !result.CreatedTrigger
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
	if runBranch && len(refs) > 0 {
		// A tag run on the branch's head builds that commit already; a
		// second, branch run on it would post the same contexts after the
		// tag's and could be read for the tag's build. The scaffold commit
		// of a new repository is both. The read only saves that run: when it
		// fails, the branch runs.
		head, err := gh.CommitSHA(ctx, owner, repo, branch)
		var unread []string
		if err != nil {
			unread = append(unread, fmt.Sprintf("the head of %s (%v)", branch, err))
		}
		for _, ref := range refs {
			if err != nil {
				break
			}
			sha, terr := gh.CommitSHA(ctx, owner, repo, ref.Tag)
			if terr != nil {
				unread = append(unread, fmt.Sprintf("the commit of %s (%v)", ref.Tag, terr))
				continue
			}
			if sha == head {
				runBranch = false
				progress("%s: %s is the head of %s: its run builds the branch's commit too", slug, ref.Tag, branch)
				break
			}
		}
		if runBranch && len(unread) > 0 {
			progress("%s: could not read %s: running the branch too", slug, strings.Join(unread, ", "))
		}
	}
	if runBranch {
		refs = append(refs, circleciclient.Ref{Branch: branch})
	}

	if result.FollowedProject && len(refs) > 0 {
		// Right before the first run, so nothing after the cancel can stop
		// the run that replaces the follow's build.
		cancelFollowBuild(ctx, ci, opts.Waiter.Sleep, owner, repo, branch, report)
		if len(report.CancelledFollowWorkflows) > 0 {
			progress("%s: cancelled the follow's OAuth build of %s (%s): the GitHub App pipeline's run of it posts the newer statuses", slug, branch, strings.Join(report.CancelledFollowWorkflows, ", "))
		}
		if report.FollowBuildUncancelled != "" {
			progress("%s: could not confirm the follow's OAuth build of %s was cancelled (%s): when it finishes after the GitHub App pipeline's run, its statuses show on %s until its next push", slug, branch, report.FollowBuildUncancelled, branch)
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

// followBuildReads bounds the reads of the follow's build, followBuildWait
// is the wait between them: the build is listed a moment after the follow,
// and its setup workflow continues it a little later.
const (
	followBuildReads = 10
	followBuildWait  = 3 * time.Second
)

// cancelFollowBuild cancels the running workflows of the OAuth build of
// branch that the follow creating the project started, as the person. The
// App pipeline's runs that follow use the same status contexts, and GitHub
// shows the newest status of every context: a follow's build that finished
// after them would leave its statuses, often red, on the branch's head until
// its next push. Cancelled first, it ends well before the App runs do; a
// cancelled job's status may still lag a little, which nothing here waits for.
//
// The build is read again, sleep apart, until a read cancels nothing new and
// nothing more can continue it: CircleCI lists it, no setup workflow can
// still continue it, and a listing of the setup workflow alone, or of none,
// held for setupOnlyReads reads, as Wait takes it. A cancel that failed is
// tried again on the next read, unless CircleCI refused it to the person
// (403, 404), and forgotten once its workflow finished or is gone. CircleCI
// refusing the person the reads of the build (403) ends them; a 404 may be
// the moment before it knows the new project, and is read again. The project
// is seconds old, so every pipeline of branch is the follow's, whatever
// CircleCI types its trigger. A push in the moments since the follow builds
// on the App pipeline alone and is cancelled with it: the branch run builds
// the branch's newest head again. A failure is noted in report, not
// returned: the branch run still follows, only the order of the statuses is
// at stake. A nil sleep waits followBuildWait.
func cancelFollowBuild(ctx context.Context, ci CircleCI, sleep func(context.Context, time.Duration) error, owner, repo, branch string, report *Report) {
	if sleep == nil {
		sleep = sleepFor
	}
	b := &followBuild{ci: ci, owner: owner, repo: repo, branch: branch, report: report, cancelled: map[string]bool{}, failed: map[string]string{}, refused: map[string]bool{}}
	pending := ""
	for read := 1; ; read++ {
		var settled bool
		if settled, pending = b.pass(ctx); settled || b.refusedRead || read >= followBuildReads {
			break
		}
		if err := sleep(ctx, followBuildWait); err != nil {
			wait := fmt.Sprintf("the wait for further reads of the build of %s ended: %v", branch, err)
			if pending == "" {
				pending = wait
			} else {
				pending += "; " + wait
			}
			break
		}
	}
	problems := slices.Sorted(maps.Values(b.failed))
	if pending != "" {
		problems = append(problems, pending)
	}
	report.FollowBuildUncancelled = strings.Join(problems, "; ")
}

// followBuild is what the reads of the follow's build found: the workflows
// cancelled, the cancels refused by workflow id, and how many reads in a row
// listed the setup workflow alone, or none, on a settled pipeline. refused
// are the failed cancels CircleCI refused to the person, not tried again;
// refusedRead says it refused the person every read of the build (403), which
// ends the reads.
type followBuild struct {
	ci                  CircleCI
	owner, repo, branch string
	report              *Report
	cancelled           map[string]bool
	failed              map[string]string
	refused             map[string]bool
	refusedRead         bool
	quiet               int
}

// pass reads the follow's build once and cancels its running workflows. It
// says whether the build is settled, and what keeps it from being so.
func (b *followBuild) pass(ctx context.Context) (bool, string) {
	page, err := b.ci.ListBranchPipelines(ctx, b.owner, b.repo, b.branch, "")
	if err != nil {
		b.quiet = 0
		b.refusedRead = circleciclient.IsForbidden(err)
		return false, fmt.Sprintf("listing the pipelines of %s: %v", b.branch, err)
	}
	found, newCancel, sparse := false, false, false
	pending := ""
	seen := map[string]bool{}
	unread, read, forbidden := false, false, false
	for _, p := range page.Items {
		found = true
		workflows, err := b.ci.ListPipelineWorkflows(ctx, p.ID)
		if err != nil {
			pending = fmt.Sprintf("listing the workflows of pipeline %d: %v", p.Number, err)
			unread = true
			forbidden = forbidden || circleciclient.IsForbidden(err)
			continue
		}
		read = true
		setupStopped := false
		for _, wf := range workflows {
			seen[wf.ID] = true
			if !circleciclient.WorkflowRunning(wf.Status) {
				// Finished on its own: a refused cancel no longer matters.
				delete(b.failed, wf.ID)
				if wf.Tag == circleciclient.WorkflowTagSetup && !circleciclient.WorkflowSucceeded(wf.Status) {
					setupStopped = true
				}
				continue
			}
			if !b.cancelled[wf.ID] && !b.refused[wf.ID] {
				if err := b.ci.CancelWorkflow(ctx, wf.ID); err != nil {
					b.failed[wf.ID] = fmt.Sprintf("cancelling workflow %s: %v", wf.Name, err)
					// The workflow was just listed: a 404 is no lag of a new
					// project, it is gone.
					b.refused[wf.ID] = circleciclient.IsForbidden(err) || circleciclient.IsNotFound(err)
					continue
				}
				delete(b.failed, wf.ID)
				b.cancelled[wf.ID] = true
				newCancel = true
				if !slices.Contains(b.report.CancelledFollowWorkflows, wf.Name) {
					b.report.CancelledFollowWorkflows = append(b.report.CancelledFollowWorkflows, wf.Name)
				}
			}
			if wf.Tag == circleciclient.WorkflowTagSetup {
				setupStopped = true
			}
		}
		switch {
		case p.State == "errored":
			// Its configuration did not compile: nothing runs.
		case circleciclient.PipelineContinuing(p.State) && !setupStopped:
			if pending == "" {
				pending = fmt.Sprintf("the build of %s had not continued", b.branch)
			}
		case len(workflows) == 0 || circleciclient.SetupOnly(workflows):
			// A continuation may still create workflows the listing lags
			// behind, also one submitted as its setup workflow was cancelled.
			sparse = true
		}
	}
	// A refusal ends the reads only when nothing of the build could be read.
	b.refusedRead = forbidden && !read
	if !unread {
		// A workflow the build no longer lists cannot be cancelled.
		for id := range b.failed {
			if !seen[id] {
				delete(b.failed, id)
			}
		}
	}
	retry := false
	for id := range b.failed {
		retry = retry || !b.refused[id]
	}
	switch {
	case !found:
		b.quiet = 0
		return false, fmt.Sprintf("CircleCI listed no build of %s", b.branch)
	case pending != "" || newCancel || retry:
		// A read that cancelled anything is followed by another: the
		// cancelled setup workflow may have continued the build already.
		// When the reads run out on it, what it cancelled is the report.
		b.quiet = 0
		return false, pending
	case sparse:
		if b.quiet++; b.quiet < setupOnlyReads {
			return false, fmt.Sprintf("the build of %s had not continued", b.branch)
		}
	}
	return true, ""
}

// sleepFor waits d, or until ctx ends.
func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
