package reconcile

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// CircleCIAppLogin is the login the CircleCI GitHub App posts its commit
// statuses as. A repository whose CircleCI statuses come from it builds on
// a GitHub App pipeline: checkout, events and statuses go through the App's
// installation, and whoever created the pipeline is not load-bearing.
const CircleCIAppLogin = "circleci-app[bot]"

// stepCircleCI checks that the repository builds on a CircleCI GitHub App
// pipeline, and writes nothing: the reconciler holds no CircleCI identity.
// CircleCI has no service accounts, so the pipeline is created by a person,
// as that person: `devctl repo follow-circleci`, which `devctl repo watch`
// runs for the creator of a new repository. A missing pipeline is the
// finding FindingCircleCIPipelineMissing naming that command; an OAuth follow,
// alone or beside the App pipeline, is the advisory FindingCircleCIOAuthFollow:
// it builds today, and stops when the follower leaves.
//
// With a CircleCI client (a person's `devctl repo reconcile`) the project's
// pipeline definitions are read: a GitHub App definition with an enabled
// trigger is the pipeline. Without one (the reconciler) the CircleCI
// statuses say it, read on the default branch's recent commits, else the
// latest release's tag, else recently merged pull requests' heads
// (recentCircleCIStatuses): statuses posted by CircleCIAppLogin are a GitHub
// App pipeline's, statuses posted by a person are an OAuth follow's, which
// stops building when that person leaves.
func (r *Runner) stepCircleCI(ctx context.Context, s *run, sr *StepResult) error {
	if r.CircleCI != nil {
		return r.circleCIDefinitions(ctx, s, sr)
	}
	return r.circleCIFromStatuses(ctx, s, sr)
}

// followCircleCIFix is the fix of FindingCircleCIPipelineMissing: the
// person's command, with --migrate when an OAuth trigger builds the
// repository today and would build every push a second time.
func followCircleCIFix(slug string, migrate bool) string {
	cmd := "devctl repo follow-circleci " + slug
	if migrate {
		cmd += " --migrate"
	}
	return fmt.Sprintf("`%s` as a member of the owning team: it creates the GitHub App pipeline and its all-pushes trigger with your CircleCI login (devctl auth login --circleci-only, Write access), runs the default branch and an unbuilt latest release, and waits for green; whoever runs it is not needed afterwards", cmd)
}

// circleCIDefinitions is the circleci step read through CircleCI: the
// project's pipeline definitions and their triggers.
func (r *Runner) circleCIDefinitions(ctx context.Context, s *run, sr *StepResult) error {
	project, err := r.CircleCI.GetProject(ctx, s.owner, s.name)
	switch {
	case circleciclient.IsNotFound(err):
		s.report(sr, FindingCircleCIPipelineMissing,
			fmt.Sprintf("CircleCI knows no project %s: nothing builds it", s.slug()),
			followCircleCIFix(s.slug(), false))
		sr.Summary = "no CircleCI project"
		return nil
	case err != nil:
		return err
	}
	defs, err := r.CircleCI.ListPipelineDefinitions(ctx, project.ID)
	if err != nil {
		return err
	}
	var app string
	var oauth []string
	for _, d := range defs {
		triggers, err := r.CircleCI.ListTriggers(ctx, project.ID, d.ID)
		if err != nil {
			return err
		}
		enabled := slices.ContainsFunc(triggers, func(t circleciclient.Trigger) bool { return !t.Disabled })
		switch {
		case !enabled:
		case d.GitHubApp() && app == "":
			app = d.Name
		case !d.GitHubApp():
			oauth = append(oauth, d.Name)
		}
	}
	switch {
	case app != "" && len(oauth) > 0:
		s.report(sr, FindingCircleCIOAuthFollow,
			fmt.Sprintf("%s builds on the GitHub App pipeline %s and on the OAuth pipeline %s: every push builds twice", s.slug(), app, strings.Join(oauth, ", ")),
			followCircleCIFix(s.slug(), true))
		sr.Summary = fmt.Sprintf("GitHub App pipeline %s; OAuth pipeline %s triggered too", app, strings.Join(oauth, ", "))
	case app != "":
		sr.Summary = fmt.Sprintf("GitHub App pipeline %s", app)
	case len(oauth) > 0:
		s.report(sr, FindingCircleCIOAuthFollow,
			fmt.Sprintf("%s builds on the OAuth pipeline %s, through the GitHub grant of whoever follows it, and stops building when they leave", s.slug(), strings.Join(oauth, ", ")),
			followCircleCIFix(s.slug(), true))
		sr.Summary = "OAuth follow, no GitHub App pipeline"
	default:
		s.report(sr, FindingCircleCIPipelineMissing,
			fmt.Sprintf("%s has no CircleCI pipeline with an enabled trigger: nothing builds it", s.slug()),
			followCircleCIFix(s.slug(), false))
		sr.Summary = "no GitHub App pipeline"
	}
	return nil
}

// circleCIFromStatuses is the circleci step read from GitHub: who posted the
// CircleCI statuses of the default branch's recent commits.
func (r *Runner) circleCIFromStatuses(ctx context.Context, s *run, sr *StepResult) error {
	statuses, where, src, err := r.recentCircleCIStatuses(ctx, s)
	if err != nil {
		return err
	}
	app, others := CircleCIPosters(statuses)
	switch {
	case app && len(others) > 0:
		s.report(sr, FindingCircleCIOAuthFollow,
			fmt.Sprintf("the CircleCI statuses of %s on %s come from %s and from %s: it builds on the GitHub App pipeline and on an OAuth follow, so every push builds twice", s.slug(), where, CircleCIAppLogin, strings.Join(others, ", ")),
			followCircleCIFix(s.slug(), true))
		sr.Summary = fmt.Sprintf("CircleCI statuses on %s from %s and an OAuth follow", where, CircleCIAppLogin)
	case app:
		sr.Summary = fmt.Sprintf("CircleCI statuses on %s from %s: GitHub App pipeline", where, CircleCIAppLogin)
	case len(others) > 0:
		s.report(sr, FindingCircleCIOAuthFollow,
			fmt.Sprintf("the CircleCI statuses of %s on %s come from %s: an OAuth follow, which builds through that person's GitHub grant and stops when they leave", s.slug(), where, strings.Join(others, ", ")),
			followCircleCIFix(s.slug(), true))
		sr.Summary = "OAuth follow, no GitHub App pipeline"
	case len(src.refused) > 0:
		s.report(sr, FindingUnchecked,
			fmt.Sprintf("no CircleCI status on %s in what this identity could read (%s), and GitHub refused %s: whether a CircleCI pipeline builds it is unknown", s.slug(), strings.Join(src.read, ", "), strings.Join(src.refused, ", ")),
			"grant the identity the run uses contents: read, pull_requests: read and statuses: read on the repository (for the reconciler, its GitHub App), then run the check again")
		sr.Summary = "CircleCI pipeline not checked: " + strings.Join(src.refused, ", ") + " not readable"
	default:
		s.report(sr, FindingCircleCIPipelineMissing,
			fmt.Sprintf("no CircleCI status on %s: not on %s; no CircleCI pipeline builds it", s.slug(), strings.Join(src.read, ", ")),
			followCircleCIFix(s.slug(), false))
		sr.Summary = "no CircleCI status on " + strings.Join(src.read, ", ")
	}
	return nil
}

// recentCommits is how many commits of the default branch the circleci step
// reads for CircleCI statuses without a client: a commit pushed seconds ago
// has none yet, and neither has a [skip ci] commit.
const recentCommits = 5

// recentPullRequests is how many recently merged pull requests' heads the
// circleci step reads when the default branch carries no CircleCI status: a
// pipeline whose workflows ignore the default branch (branches: ignore:
// main) builds the pull requests' branches and the release tags only.
const recentPullRequests = 3

// statusSources records which sources the circleci step read for CircleCI
// statuses without a client, and which GitHub refused, so its verdict names
// exactly what it looked at.
type statusSources struct {
	read, refused []string
}

// recentCircleCIStatuses returns the CircleCI statuses the circleci step
// decides on without a client, and where they were read: the newest of the
// default branch's last recentCommits commits that carries any (the head
// first); else the latest release's tag and the head commits of the last
// recentPullRequests pull requests merged into the default branch, the
// newest of them first by publish and merge time. None when none of them carries one; src says what was read
// and what GitHub refused (404 or 403: an empty repository, an identity
// without pull-request read), which is passed over, not failed on. There is
// no age limit: a quiet repository whose pipeline ignores the default branch
// is built by its old pull requests' pipeline still, and a pipeline that
// stopped leaves an OAuth follow's statuses, which read as the advisory
// circleci-oauth-follow.
func (r *Runner) recentCircleCIStatuses(ctx context.Context, s *run) ([]*github.RepoStatus, string, statusSources, error) {
	var src statusSources
	statuses, err := r.circleCIStatuses(ctx, s, s.branch())
	if err != nil || len(statuses) > 0 {
		return statuses, s.branch(), src, err
	}
	commits, resp, err := r.GitHub.Repositories.ListCommits(ctx, s.owner, s.name, &github.CommitsListOptions{SHA: s.branch(), ListOptions: github.ListOptions{PerPage: recentCommits}})
	switch {
	case resp != nil && resp.StatusCode == http.StatusConflict:
		src.read = append(src.read, s.branch()+" (empty)")
	case isNotFound(resp, err) || isForbidden(err):
		src.read = append(src.read, "the head of "+s.branch())
		src.refused = append(src.refused, "the commits of "+s.branch())
	case err != nil:
		return nil, "", src, err
	default:
		if n := min(len(commits), recentCommits); n > 1 {
			src.read = append(src.read, fmt.Sprintf("the last %d commits of %s", n, s.branch()))
		} else {
			src.read = append(src.read, "the head of "+s.branch())
		}
	}
	for i, c := range commits {
		if i == 0 || i >= recentCommits {
			continue // the head is the branch's, read above
		}
		statuses, err := r.circleCIStatuses(ctx, s, c.GetSHA())
		if err != nil || len(statuses) > 0 {
			return statuses, s.branch(), src, err
		}
	}

	// The latest release and the recently merged pull requests are read
	// newest first, by publish and merge time: a repository moved to the App
	// pipeline after its last release reads by its newer pull requests.
	type candidate struct {
		ref, where string
		at         time.Time
		pull       bool
	}
	var candidates []candidate
	release, resp, err := r.GitHub.Repositories.GetLatestRelease(ctx, s.owner, s.name)
	switch {
	case isNotFound(resp, err):
	case isForbidden(err):
		src.refused = append(src.refused, "the latest release")
	case err != nil:
		return nil, "", src, err
	case release.TagName != "":
		candidates = append(candidates, candidate{release.TagName, "the latest release " + release.TagName, publishedAt(release), false})
	}
	merged, refused, err := r.recentlyMerged(ctx, s)
	switch {
	case err != nil:
		return nil, "", src, err
	case refused:
		src.refused = append(src.refused, "the pull requests")
	}
	for _, pr := range merged {
		candidates = append(candidates, candidate{pr.GetHead().GetSHA(), fmt.Sprintf("the head of the merged pull request #%d", pr.GetNumber()), pr.MergedAt.Time, true})
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return b.at.Compare(a.at) })
	// What was read is recorded once its statuses were: a refused read is
	// named as refused, not as read.
	pulls := 0
	for _, c := range candidates {
		statuses, err := r.circleCIStatuses(ctx, s, c.ref)
		switch {
		case isForbidden(err):
			src.refused = append(src.refused, "the statuses of "+c.where)
			continue
		case err != nil:
			return nil, "", src, err
		case len(statuses) > 0:
			return statuses, c.where, src, nil
		case c.pull:
			pulls++
		default:
			src.read = append(src.read, c.where)
		}
	}
	switch {
	case pulls > 1:
		src.read = append(src.read, fmt.Sprintf("the heads of the last %d pull requests merged into %s", pulls, s.branch()))
	case pulls == 1:
		src.read = append(src.read, "the head of a pull request merged into "+s.branch())
	}
	return nil, "", src, nil
}

// recentlyMerged returns the last recentPullRequests pull requests merged
// into the default branch, newest merge first; refused says GitHub refused
// the list (404 or 403). GitHub lists closed pull requests by their last
// update, and a comment or a label moves a long-merged one to the top, so a
// page of them is read and sorted by the merge time.
func (r *Runner) recentlyMerged(ctx context.Context, s *run) (merged []*github.PullRequest, refused bool, err error) {
	pulls, resp, err := r.GitHub.PullRequests.List(ctx, s.owner, s.name, &github.PullRequestListOptions{
		// One page of closed pull requests, by last update: merged and
		// unmerged alike, so a repository that closes many pull requests
		// unmerged (Renovate's superseded ones) may find fewer than
		// recentPullRequests merged ones in it.
		State: "closed", Base: s.branch(), Sort: "updated", Direction: "desc", ListOptions: github.ListOptions{PerPage: 50},
	})
	switch {
	case isNotFound(resp, err) || isForbidden(err):
		return nil, true, nil
	case err != nil:
		return nil, false, err
	}
	for _, pr := range pulls {
		if pr.MergedAt != nil && pr.GetHead().GetSHA() != "" {
			merged = append(merged, pr)
		}
	}
	slices.SortStableFunc(merged, func(a, b *github.PullRequest) int { return b.MergedAt.Compare(a.MergedAt.Time) })
	if len(merged) > recentPullRequests {
		merged = merged[:recentPullRequests]
	}
	return merged, false, nil
}

// circleCIStatusPages bounds the statuses read of one commit: three pages
// of a hundred hold a tag build and a branch build several times over.
const circleCIStatusPages = 3

// circleCIStatuses returns the CircleCI statuses of ref's commit
// (ListCircleCIStatuses).
func (r *Runner) circleCIStatuses(ctx context.Context, s *run, ref string) ([]*github.RepoStatus, error) {
	return ListCircleCIStatuses(ctx, r.GitHub, s.owner, s.name, ref)
}

// ListCircleCIStatuses returns the CircleCI statuses of ref's commit in
// owner/repo, newest first, as GitHub lists them; none for a ref GitHub does
// not know.
func ListCircleCIStatuses(ctx context.Context, gh *github.Client, owner, repo, ref string) ([]*github.RepoStatus, error) {
	var out []*github.RepoStatus
	opts := &github.ListOptions{PerPage: 100}
	for range circleCIStatusPages {
		page, resp, err := gh.Repositories.ListStatuses(ctx, owner, repo, ref, opts)
		switch {
		case isNotFound(resp, err):
			return out, nil
		case err != nil:
			return nil, err
		}
		for _, st := range page {
			if strings.HasPrefix(st.GetContext(), CircleCIContextPrefix) {
				out = append(out, st)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// AppStatuses are the statuses CircleCIAppLogin posted: the GitHub App
// pipeline's, each linking its workflow.
func AppStatuses(statuses []*github.RepoStatus) []*github.RepoStatus {
	var out []*github.RepoStatus
	for _, st := range statuses {
		if st.GetCreator().GetLogin() == CircleCIAppLogin {
			out = append(out, st)
		}
	}
	return out
}

// OAuthBuiltSince says an OAuth follow, a poster other than
// CircleCIAppLogin, posted a status no earlier than a release published at
// publishedAt (less tagBuildSlack): its job ran after the tag and may have
// been the tag's. A status without a creator counts as nobody's.
func OAuthBuiltSince(statuses []*github.RepoStatus, publishedAt time.Time) bool {
	since := publishedAt.Add(-tagBuildSlack)
	for _, st := range statuses {
		login := st.GetCreator().GetLogin()
		if login != "" && login != CircleCIAppLogin && !st.GetCreatedAt().Before(since) {
			return true
		}
	}
	return false
}

// CircleCIPosters says who posted statuses: whether CircleCIAppLogin did,
// and the other logins, sorted.
func CircleCIPosters(statuses []*github.RepoStatus) (app bool, others []string) {
	for _, st := range statuses {
		login := st.GetCreator().GetLogin()
		switch {
		case login == CircleCIAppLogin:
			app = true
		case login != "" && !slices.Contains(others, login):
			others = append(others, login)
		}
	}
	slices.Sort(others)
	return app, others
}

// tagBuildSlack is how much earlier than its release a status of the tag's
// own build may be: auto-release creates the tag and the release in one
// call, and CircleCI's first status follows the tag push within seconds
// (2 s on release-automation-sandbox v0.2.4).
const tagBuildSlack = 10 * time.Second

// setupContext is the status context of a dynamic-config pipeline's setup
// workflow: the first workflow of every pipeline the generated configuration
// starts, one per pipeline.
const setupContext = CircleCIContextPrefix + "setup"

// TagBuildStatuses are the statuses of the build of a release tag among the
// CircleCI statuses of its commit, newest first: the newest of every
// context. A commit's statuses are the commit's, not the ref's: the branch
// build of the commit the tag points at posts the same contexts, and keeps
// posting after the tag when its jobs finish late. So the statuses are read
// by workflow (StatusWorkflow), and a workflow is the tag's when its pipeline
// started after the release (publishedAt, less tagBuildSlack).
//
// A dynamic-config pipeline, the generated one, starts with one setup
// workflow (setupContext), and its continuation starts the build's
// workflows once the setup succeeded, under workflow ids of their own. Each
// such workflow belongs to the setup workflow that settled last before it
// started: the branch build's continuation, which may start after the
// release, belongs to the branch build's setup, which started before it, and
// drops out. A commit without a setup status (a hand-kept, static
// configuration) is read by each workflow's own first status. A branch
// pipeline started on the same commit after the release (a person's run on
// the default branch) is not told apart from the tag's: statuses do not
// name the ref.
func TagBuildStatuses(statuses []*github.RepoStatus, publishedAt time.Time) []*github.RepoStatus {
	type workflow struct {
		first, settled time.Time
		setup          bool
	}
	workflows := map[string]*workflow{}
	for _, st := range statuses {
		id := StatusWorkflow(st.GetTargetURL())
		w := workflows[id]
		if w == nil {
			w = &workflow{first: st.GetCreatedAt().Time}
			workflows[id] = w
		}
		at := st.GetCreatedAt().Time
		if at.Before(w.first) {
			w.first = at
		}
		if st.GetContext() == setupContext {
			w.setup = true
			if st.GetState() != "pending" && (w.settled.IsZero() || at.Before(w.settled)) {
				w.settled = at
			}
		}
	}
	since := publishedAt.Add(-tagBuildSlack)
	tags := map[string]bool{}
	for id, w := range workflows {
		if !w.setup {
			continue
		}
		tags[id] = !w.first.Before(since)
	}
	tag := func(id string) bool {
		w := workflows[id]
		if w.setup || len(tags) == 0 {
			return !w.first.Before(since)
		}
		// The setup workflow that settled last before this one started.
		owner, settled := "", time.Time{}
		for sid, sw := range workflows {
			if !sw.setup || sw.settled.IsZero() || sw.settled.After(w.first) {
				continue
			}
			// GitHub's timestamps are to the second: two setups settling in
			// the same second break on the workflow id, so the verdict does
			// not depend on map order.
			if sw.settled.After(settled) || (sw.settled.Equal(settled) && sid < owner) {
				owner, settled = sid, sw.settled
			}
		}
		if owner == "" {
			return !w.first.Before(since)
		}
		return tags[owner]
	}
	var out []*github.RepoStatus
	seen := map[string]bool{}
	for _, st := range statuses {
		if seen[st.GetContext()] || !tag(StatusWorkflow(st.GetTargetURL())) {
			continue
		}
		seen[st.GetContext()] = true
		out = append(out, st)
	}
	return out
}

// StatusWorkflow is the workflow a CircleCI status's target URL names: the
// workflow id of a GitHub App status
// (https://app.circleci.com/workflow/<id>/job/<id>) or of a pipeline URL
// (…/pipelines/<vcs>/<org>/<repo>/<n>/workflows/<id>/jobs/<n>). An OAuth
// status names its job alone (https://circleci.com/gh/<org>/<repo>/<n>), and
// the URL itself stands for its workflow.
func StatusWorkflow(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if (p == "workflow" || p == "workflows") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return target
}

// circleCIConfig is the pipeline's entry point on the default branch.
const circleCIConfig = ".circleci/config.yml"

// hasPipeline says whether the repository has a CircleCI pipeline for the
// circleci and release steps to act on: the entry declares a generated one
// (gen.ci.generate: true with a job for the CircleCI generator,
// [reposetup.GeneratesCircleCI] — on a first creation the config is not on
// the branch yet), or .circleci/config.yml is on the default branch.
// Generated CI without a CircleCI job is GitHub Actions only, and the
// branch decides for it as for an undeclared pipeline. The field
// is read as declared: an existing entry without gen.ci keeps the
// repository's own CircleCI configuration, so the branch decides for it
// (the validator writes the creation default for an entry being added
// only). A configuration repository, or one released by GitHub Actions,
// has neither: CircleCI has nothing to build there. A template repository
// whose configuration is content for the repositories created from it
// says so (gen.ci.templateContent), and the branch is not asked. The
// branch is read once per run, the two steps share the answer.
func (r *Runner) hasPipeline(ctx context.Context, s *run) (bool, error) {
	if s.pipeline != nil {
		return *s.pipeline, nil
	}
	if templateContent(s.fields) {
		s.pipeline = new(false)
		return false, nil
	}
	if reposetup.GeneratesCircleCI(s.fields) {
		s.pipeline = new(true)
		return true, nil
	}
	_, found, err := r.fileContent(ctx, s.owner, s.name, circleCIConfig, s.branch())
	if err != nil {
		return false, err
	}
	s.pipeline = &found
	return found, nil
}

// templateContent says the entry declares its .circleci/config.yml as
// content for the repositories created from this template
// (gen.ci.templateContent): CircleCI has nothing to build here, whatever
// the branch carries.
func templateContent(fields reposetup.Fields) bool {
	g := fields.Gen
	return g != nil && g.CI != nil && g.CI.TemplateContent
}

// stepRelease verifies the latest release from the GitHub App pipeline's
// CircleCI statuses on its tag's commit, read on GitHub: no CircleCI identity
// is needed. Of the commit's statuses those of the tag's own build count
// (TagBuildStatuses). A commit built by an OAuth follow alone is skipped,
// not verified: an OAuth status names its job, not its workflow, so a branch
// job cannot be told from the tag's. None is a missed build;
// the fix is `devctl repo follow-circleci`, which runs an unbuilt latest
// release as the person, or the next tag. A failed one is a release nothing
// was published for; the fix is a rerun of the failed workflow from failed
// — the rerun checks out the same commit and runs the publish jobs — or,
// when the cause is in the code, the next tag. Either is a finding, never a
// rebuild: the reconciler publishes nothing a person did not ask for. A
// build red only in GitHubReleaseJob is a published release: the GitHub
// release this step read is that job's one output, and every artifact was
// pushed before it.
//
// The release verified is one of the platform's flow, a vX.Y.Z tag
// (isReleaseTag). A latest release tagged otherwise — per component,
// base/v0.1.0 — is outside that flow: the step is skipped naming the tag,
// nothing is read and nothing is reported. A multi-image entry releases one
// line per image instead (stepImageReleases).
func (r *Runner) stepRelease(ctx context.Context, s *run, sr *StepResult) error {
	if s.hasFlavour(flavourMultiImage) {
		return r.stepImageReleases(ctx, s, sr)
	}
	release, resp, err := r.GitHub.Repositories.GetLatestRelease(ctx, s.owner, s.name)
	switch {
	case isNotFound(resp, err):
		sr.Summary = "no release yet"
		return nil
	case err != nil:
		return err
	}
	tag := release.GetTagName()
	if !isReleaseTag(tag) {
		sr.Verdict = VerdictSkipped
		sr.Summary = fmt.Sprintf("release %s: not a vX.Y.Z tag, not verified", tag)
		return nil
	}
	summary, oauth, err := r.verifyRelease(ctx, s, sr, tag, publishedAt(release))
	if oauth {
		sr.Verdict = VerdictSkipped
	}
	sr.Summary = summary
	return err
}

// stepImageReleases is the release step of a multi-image entry: every image
// is its own release line, <name>/vX.Y.Z, cut by the multi-image
// auto-release and built by the tag pipeline of that image. GitHub's latest
// release is the newest of any line, so the step verifies the newest
// published release of each line (imageReleases), each as stepRelease
// verifies a single line's, the newest release first.
func (r *Runner) stepImageReleases(ctx context.Context, s *run, sr *StepResult) error {
	releases, err := r.imageReleases(ctx, s)
	if err != nil {
		return err
	}
	if len(releases) == 0 {
		sr.Summary = "no <name>/vX.Y.Z release yet"
		return nil
	}
	// GitHub's latest release leads the summary, the others follow by image
	// name: a reader of GitHub's latest release finds it built at the start.
	newest, err := r.latestImageRelease(ctx, s, releases)
	if err != nil {
		return err
	}
	summary, oauth, err := r.verifyRelease(ctx, s, sr, newest.GetTagName(), publishedAt(newest))
	if err != nil {
		return err
	}
	summaries, allOAuth := []string{summary}, oauth
	for _, release := range releases {
		if release == newest {
			continue
		}
		summary, oauth, err := r.verifyRelease(ctx, s, sr, release.GetTagName(), publishedAt(release))
		if err != nil {
			return err
		}
		summaries, allOAuth = append(summaries, summary), allOAuth && oauth
	}
	if allOAuth {
		sr.Verdict = VerdictSkipped
	}
	sr.Summary = strings.Join(summaries, "; ")
	return nil
}

// latestImageRelease is the line release of releases that GitHub names its
// latest release, the one the inventory reads: GitHub orders releases by
// their commit's date, so the releases of two images cut from one commit tie
// and only GitHub's own answer says which is latest. A latest release
// outside every line (a plain vX.Y.Z) leaves the newest line release.
func (r *Runner) latestImageRelease(ctx context.Context, s *run, releases []*github.RepositoryRelease) (*github.RepositoryRelease, error) {
	latest, resp, err := r.GitHub.Repositories.GetLatestRelease(ctx, s.owner, s.name)
	if err != nil && !isNotFound(resp, err) {
		return nil, err
	}
	newest := releases[0]
	for _, release := range releases {
		if release.GetTagName() == latest.GetTagName() {
			return release, nil
		}
		if release.GetCreatedAt().After(newest.GetCreatedAt().Time) {
			newest = release
		}
	}
	return newest, nil
}

// imageReleases returns the newest published release of each image line of
// a multi-image repository, ordered by image name. Drafts and pre-releases
// are left out, as GitHub's latest release leaves them out; a release
// tagged outside the <name>/vX.Y.Z grammar (imageReleaseTag) belongs to no
// line.
func (r *Runner) imageReleases(ctx context.Context, s *run) ([]*github.RepositoryRelease, error) {
	newest := map[string]*github.RepositoryRelease{}
	versions := map[string]*semver.Version{}
	opts := &github.ListOptions{PerPage: 100}
	for {
		page, resp, err := r.GitHub.Repositories.ListReleases(ctx, s.owner, s.name, opts)
		if err != nil {
			return nil, err
		}
		for _, release := range page {
			if release.GetDraft() || release.GetPrerelease() {
				continue
			}
			image, version, ok := imageReleaseTag(release.GetTagName())
			if !ok {
				continue
			}
			if v, seen := versions[image]; !seen || version.GreaterThan(v) {
				newest[image], versions[image] = release, version
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	images := make([]string, 0, len(newest))
	for image := range newest {
		images = append(images, image)
	}
	slices.Sort(images)
	out := make([]*github.RepositoryRelease, 0, len(images))
	for _, image := range images {
		out = append(out, newest[image])
	}
	return out, nil
}

// publishedAt is when a release was published: when auto-release created
// its tag. A release's created_at is its commit's date, not the release's.
func publishedAt(release *github.RepositoryRelease) time.Time {
	if at := release.GetPublishedAt(); !at.IsZero() {
		return at.Time
	}
	return release.GetCreatedAt().Time
}

// TagBuildGrace is how long after its release a tag may carry no status of
// its own build yet without being a missed build: CircleCI posts the first
// status within seconds of the tag push.
const TagBuildGrace = 2 * time.Minute

// verifyRelease verifies one release, tag, of the platform's flow, as
// stepRelease describes: findings go on sr, the line on the state found is
// returned.
func (r *Runner) verifyRelease(ctx context.Context, s *run, sr *StepResult, tag string, published time.Time) (summary string, oauth bool, err error) {
	statuses, err := r.circleCIStatuses(ctx, s, tag)
	if err != nil {
		return "", false, err
	}
	app := AppStatuses(statuses)
	if _, others := CircleCIPosters(statuses); len(app) == 0 && len(others) > 0 {
		// An OAuth status names its job alone, not its workflow or ref: a
		// branch job that starts after the release cannot be told from the
		// tag's. The repository carries circleci-oauth-follow already.
		return fmt.Sprintf("release %s: built on an OAuth follow (%s), not verified from statuses; `devctl repo follow-circleci %s --migrate` moves it to the GitHub App pipeline", tag, strings.Join(others, ", "), s.slug()), true, nil
	}
	build := TagBuildStatuses(app, published)
	if len(build) == 0 && OAuthBuiltSince(statuses, published) {
		// No App build after the release, but an OAuth follow's: the tag
		// may have been built there, and an OAuth status cannot say.
		_, others := CircleCIPosters(statuses)
		return fmt.Sprintf("release %s: built on an OAuth follow (%s), not verified from statuses; `devctl repo follow-circleci %s --migrate` moves it to the GitHub App pipeline", tag, strings.Join(others, ", "), s.slug()), true, nil
	}
	if len(build) == 0 {
		if r.now().Sub(published) < TagBuildGrace {
			return fmt.Sprintf("release %s: published %s ago, no status of its build yet", tag, r.now().Sub(published).Round(time.Second)), false, nil
		}
		s.report(sr, FindingMissedTagBuild,
			fmt.Sprintf("release %s of %s carries no CircleCI status of a build started after the release: nothing was built or published for the tag", tag, s.slug()),
			fmt.Sprintf("`devctl repo follow-circleci %s`: it runs the tag on the GitHub App pipeline as you, creating the pipeline first when there is none; or cut the next tag", s.slug()))
		return fmt.Sprintf("release %s: no tag build", tag), false, nil
	}
	var failed, pending, succeeded, publishedJobs []string
	for _, st := range build {
		job := strings.TrimPrefix(st.GetContext(), CircleCIContextPrefix)
		switch st.GetState() {
		case "success":
			succeeded = append(succeeded, job)
		case "failure", "error":
			if len(UnpublishedJobs([]string{job})) == 0 {
				publishedJobs = append(publishedJobs, job)
				continue
			}
			failed = append(failed, fmt.Sprintf("%s (%s)", job, st.GetState()))
		default:
			pending = append(pending, job)
		}
	}
	slices.Sort(failed)
	slices.Sort(pending)
	slices.Sort(succeeded)
	switch {
	case len(failed) > 0:
		confirm := fmt.Sprintf(", then `devctl release wait %s %s` confirms the images and chart", s.slug(), tag)
		if _, _, image := imageReleaseTag(tag); image {
			confirm = "" // release wait follows vX.Y.Z tags only
		}
		s.report(sr, FindingRedRelease,
			fmt.Sprintf("release %s of %s is red: %s", tag, s.slug(), strings.Join(failed, ", ")),
			fmt.Sprintf("nothing was published for %s: rerun the failed workflow from failed on CircleCI (the rerun checks out the same commit and runs the publish jobs)%s; when the cause is in the code, fix it and let the next tag be the release", tag, confirm))
		return fmt.Sprintf("release %s: red", tag), false, nil
	case len(pending) > 0:
		return fmt.Sprintf("release %s: building: %s", tag, strings.Join(pending, ", ")), false, nil
	}
	summary = fmt.Sprintf("release %s built: %s succeeded", tag, strings.Join(succeeded, ", "))
	if len(publishedJobs) > 0 {
		summary += fmt.Sprintf("; %s failed, the GitHub release it publishes exists", strings.Join(publishedJobs, ", "))
	}
	return summary, false, nil
}

// GitHubReleaseJob is the generated tag pipeline's job whose one output is
// the tag's GitHub release. It runs after every artifact is pushed, so its
// failure leaves nothing unpublished once the release exists: auto-release
// creates the release before the pipeline starts, and the job then fails on
// the release already being there.
const GitHubReleaseJob = "publish-github-release"

// UnpublishedJobs are the failed jobs of a release's tag pipeline that leave
// something of the release unpublished: every one but GitHubReleaseJob. The
// caller has read the GitHub release; a red tag pipeline whose failed jobs
// all drop out is a published release, not red-release.
func UnpublishedJobs(failed []string) []string {
	var out []string
	for _, j := range failed {
		if j != GitHubReleaseJob {
			out = append(out, j)
		}
	}
	return out
}

// isReleaseTag says whether tag is a release of the flow the release step
// verifies: vMAJOR.MINOR.PATCH with an optional pre-release suffix
// (v1.2.3-rc.1) — what auto-release cuts and the generated pipeline's tag
// filter (/^v.*/) builds. A repository tagging otherwise, per component
// (base/v0.1.0) or without the v, releases outside that flow, and its
// latest release is not held against CircleCI.
func isReleaseTag(tag string) bool {
	if !strings.HasPrefix(tag, "v") {
		return false
	}
	_, err := semver.StrictNewVersion(strings.TrimPrefix(tag, "v"))
	return err == nil
}

// ReleaseLine reads a release tag of the platform's flow: a vX.Y.Z tag
// (isReleaseTag), line empty, or a multi-image repository's
// <name>/vX.Y.Z (imageReleaseTag), line the image's name. ok is false for any
// other tag: CircleCI's generated tag filters build none.
func ReleaseLine(tag string) (line string, version *semver.Version, ok bool) {
	if image, v, ok := imageReleaseTag(tag); ok {
		return image, v, true
	}
	if !isReleaseTag(tag) {
		return "", nil, false
	}
	v, err := semver.StrictNewVersion(strings.TrimPrefix(tag, "v"))
	if err != nil {
		return "", nil, false
	}
	return "", v, true
}

// imageNamePattern is an image name of a multi-image repository: its
// directory under toolchains/, a valid image repository path component —
// the names the generated setup workflow and auto-release accept.
var imageNamePattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// imageReleaseTag splits a release tag of a multi-image repository,
// <name>/vX.Y.Z — what its auto-release cuts and its tag pipeline builds —
// into the image name and the version; ok is false for any other tag.
func imageReleaseTag(tag string) (image string, version *semver.Version, ok bool) {
	image, rest, found := strings.Cut(tag, "/")
	if !found || !imageNamePattern.MatchString(image) || !isReleaseTag(rest) {
		return "", nil, false
	}
	version, err := semver.StrictNewVersion(strings.TrimPrefix(rest, "v"))
	if err != nil {
		return "", nil, false
	}
	return image, version, true
}
