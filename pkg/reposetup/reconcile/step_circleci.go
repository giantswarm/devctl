package reconcile

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// stepCircleCI follows the project (under the new slug after a rename),
// enables setup workflows — the generated pipeline is dynamic and fails
// without them — gives the project a deploy key to check out with, verified
// on GitHub and named in the summary with the repository's other deploy
// keys (circleCIDeployKeys), and verifies the webhook CircleCI installs on
// the follow: without it no push and no tag reaches CircleCI, and the
// project is followed in name only.
// The writes run under the admin grant CircleCI asks for (adminGrant),
// revoked when the step ends.
func (r *Runner) stepCircleCI(ctx context.Context, s *run, sr *StepResult) (err error) {
	if r.CircleCI == nil {
		sr.Verdict = VerdictSkipped
		sr.Summary = "no CircleCI client"
		return nil
	}
	followed, err := r.circleCIFollowed(ctx, s)
	if err != nil {
		return err
	}
	if followed && s.unarchive != nil {
		// The archive unfollowed the project as the token's user and the
		// project stays known to CircleCI: after the unarchive the follow
		// is that user's, and the follow brings CircleCI's deploy key back.
		followed, err = r.CircleCI.Following(ctx, s.owner, s.name)
		if err != nil {
			return err
		}
	}
	grant := &adminGrant{r: r, s: s, sr: sr}
	defer func() { err = errors.Join(err, grant.revoke(ctx)) }()
	if !followed {
		change := "follow " + s.slug()
		if s.renamed {
			change = fmt.Sprintf("follow %s under its new slug (declared as %s)", s.slug(), s.declared)
		}
		if err := grant.ensure(ctx); err != nil {
			return err
		}
		err := s.plan(sr, change, func() error {
			return r.CircleCI.Follow(ctx, s.owner, s.name)
		})
		if err != nil {
			return err
		}
		s.followedNow = true
		if s.req.Mode == ModeCheck {
			// The project's settings and keys exist once it is followed.
			sr.Changes = append(sr.Changes, "enable setup workflows", "create a deploy key")
			return nil
		}
	}

	settings, err := r.CircleCI.GetProjectSettings(ctx, s.owner, s.name)
	if err != nil {
		return err
	}
	if sw := settings.Advanced.SetupWorkflows; sw == nil || !*sw {
		if err := grant.ensure(ctx); err != nil {
			return err
		}
		err := s.plan(sr, "enable setup workflows", func() error {
			_, err := r.CircleCI.UpdateProjectSettings(ctx, s.owner, s.name, circleciclient.ProjectSettings{
				Advanced: circleciclient.AdvancedSettings{SetupWorkflows: new(true)},
			})
			return err
		})
		if err != nil {
			return err
		}
	}

	keys, err := r.CircleCI.ListCheckoutKeys(ctx, s.owner, s.name)
	if err != nil {
		return err
	}
	deployKeys, err := r.circleCIDeployKeys(ctx, s, sr)
	if err != nil {
		return err
	}
	missing := len(keys) == 0 || deployKeys.circleCIMissing
	if missing {
		if err := grant.ensure(ctx); err != nil {
			return err
		}
		err := s.plan(sr, "create a deploy key", func() error {
			_, err := r.CircleCI.CreateCheckoutKey(ctx, s.owner, s.name, circleciclient.KeyTypeDeployKey)
			return err
		})
		if err != nil {
			return err
		}
	}
	webhook, err := r.circleCIWebhook(ctx, s, sr)
	if err != nil {
		return err
	}
	if len(sr.Changes) == 0 {
		sr.Summary = "followed, setup workflows on, checkout key present, " + deployKeys.summary + ", " + webhook
	}
	return nil
}

// deployKeysRead is what the circleci step read of the repository's deploy
// keys on GitHub: the summary's words on them, and whether CircleCI's is
// missing among them.
type deployKeysRead struct {
	summary string
	// circleCIMissing says the keys were read and none is CircleCI's: the
	// checkout key CircleCI lists cannot check the repository out.
	circleCIMissing bool
}

// circleCIDeployKeys reads the repository's deploy keys on GitHub, the side
// that decides whether CircleCI checks the repository out: CircleCI keeps
// listing its checkout key after its deploy key is gone from GitHub — the
// archive deletes it, a person can — so CircleCI's list alone would read a
// repository no pipeline can clone as set up. The summary names every key
// with its access, the one place an identity without the Administration
// permission (the devctl App login answers 403 on GET
// /repos/{owner}/{repo}/keys) reads them from. Keys the identity cannot read
// (GitHub answers 404 or 403) are the finding FindingUnchecked, and
// CircleCI's list decides alone, as it did before; nothing is guessed.
func (r *Runner) circleCIDeployKeys(ctx context.Context, s *run, sr *StepResult) (deployKeysRead, error) {
	keys, resp, err := r.GitHub.Repositories.ListKeys(ctx, s.owner, s.name, &github.ListOptions{PerPage: 100})
	switch {
	case isNotFound(resp, err) || isForbidden(err):
		s.report(sr, FindingUnchecked,
			fmt.Sprintf("the deploy keys of %s are not readable by this identity (GET /repos/{owner}/{repo}/keys needs the administration permission or admin rights): whether CircleCI's deploy key is on the repository is unknown", s.slug()),
			"run the check as an identity that reads the repository's deploy keys (the reconciler's Align now); the key is verified on a later run")
		return deployKeysRead{summary: "deploy keys not readable by this identity"}, nil
	case err != nil:
		return deployKeysRead{}, err
	}
	read := deployKeysRead{circleCIMissing: true}
	var named []string
	for _, k := range keys {
		if k.GetTitle() == circleCIDeployKeyTitle {
			read.circleCIMissing = false
		}
		access := "read-write"
		if k.GetReadOnly() {
			access = "read-only"
		}
		named = append(named, fmt.Sprintf("%s (%s)", k.GetTitle(), access))
	}
	read.summary = "no deploy key on GitHub"
	if len(named) > 0 {
		read.summary = "deploy keys on GitHub: " + strings.Join(named, ", ")
	}
	return read, nil
}

// circleCIWebhookURL is the URL of the webhook CircleCI installs on a
// repository it follows: every push and tag reaches CircleCI through it.
const circleCIWebhookURL = "https://circleci.com/hooks/github"

// HasCircleCIWebhook says whether hooks carry CircleCI's webhook, active and
// subscribed to push events: what the circleci step reads as "webhook
// present" and `devctl repo follow-circleci` waits for.
func HasCircleCIWebhook(hooks []*github.Hook) bool {
	for _, h := range hooks {
		if h.GetConfig().GetURL() == circleCIWebhookURL && h.GetActive() && slices.Contains(h.Events, "push") {
			return true
		}
	}
	return false
}

// circleCIWebhook verifies the repository carries CircleCI's webhook, active
// and subscribed to push events, and returns the summary's word on it. A
// missing hook is the finding FindingCircleCIWebhookMissing: the follow
// went through, but CircleCI installs the hook only for a follow by a GitHub
// admin of the repository whose CircleCI grant carries the hook scope, so a
// follow by the reconciler's identity under a temporary admin grant leaves
// the project followed and deaf. Hooks the identity cannot read (GitHub
// answers 404 or 403 without the repository_hooks permission) are the
// finding FindingUnchecked; nothing is guessed.
func (r *Runner) circleCIWebhook(ctx context.Context, s *run, sr *StepResult) (string, error) {
	hooks, resp, err := r.readHooks(ctx, s)
	switch {
	case isNotFound(resp, err) || isForbidden(err):
		s.report(sr, FindingUnchecked,
			fmt.Sprintf("the webhooks of %s are not readable by this identity (GET /repos/{owner}/{repo}/hooks needs the repository_hooks permission or admin rights): whether CircleCI's webhook is installed is unknown", s.slug()),
			"run the check as an identity that reads the repository's webhooks (the reconciler's Align now); the webhook is verified on a later run")
		return "webhook not readable by this identity", nil
	case err != nil:
		return "", err
	}
	if HasCircleCIWebhook(hooks) {
		return "webhook present", nil
	}
	s.report(sr, FindingCircleCIWebhookMissing,
		fmt.Sprintf("%s is followed on CircleCI but carries no active CircleCI webhook (%s, push events): no push and no tag reaches CircleCI, so no branch builds and the first release tag goes unbuilt", s.slug(), circleCIWebhookURL),
		fmt.Sprintf("`devctl repo follow-circleci %s` as a GitHub admin of the repository: it follows the project again with your CircleCI login (POST /api/v1.1/project/github/%s/follow), and CircleCI installs its webhook for that follow; devctl cannot create the hook itself, CircleCI signs it with its own secret", s.slug(), s.slug()))
	return "webhook missing", nil
}

// permissionAdmin is GitHub's name for the administrator permission.
const permissionAdmin = "admin"

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

// adminGrant is the circleci step's admin grant for the CircleCI token's
// GitHub user. CircleCI takes the follow, the setup-workflows setting and a
// deploy key from a GitHub administrator of the repository only ("only a
// project's Github administrator may setup Circle"), and the reconciler's
// identity holds push through the bots team. The grant is made once, before
// the step's first write and only when the user is no administrator
// already; the step revokes it when it ends, whatever failed, so the
// identity keeps what its teams give it and no run leaves it behind.
type adminGrant struct {
	r     *Runner
	s     *run
	sr    *StepResult
	asked bool
	// grantee is the login granted admin; empty until the grant is made.
	grantee string
}

// ensure makes the grant before a write, unless it is made or needless.
func (g *adminGrant) ensure(ctx context.Context) error {
	if g.asked {
		return nil
	}
	g.asked = true
	login, err := g.r.circleCIGrantee(ctx, g.s)
	if err != nil || login == "" {
		return err
	}
	return g.s.plan(g.sr, fmt.Sprintf("grant %s admin for the CircleCI set-up, revoked after it", login), func() error {
		_, _, err := g.r.GitHub.Repositories.AddCollaborator(ctx, g.s.owner, g.s.name, login, github.AddCollaboratorRequest{Permission: new(permissionAdmin)})
		if err == nil {
			g.grantee = login
		}
		return err
	})
}

// revoke takes a grant made back; nothing when none was.
func (g *adminGrant) revoke(ctx context.Context) error {
	if g.grantee == "" {
		return nil
	}
	if _, err := g.r.GitHub.Repositories.RemoveCollaborator(ctx, g.s.owner, g.s.name, g.grantee); err != nil {
		return fmt.Errorf("revoke the admin grant of %s: %w", g.grantee, err)
	}
	return nil
}

// circleCIGrantee returns the login of the CircleCI token's GitHub user when
// that user is not an administrator of the repository; empty when no grant
// is needed.
func (r *Runner) circleCIGrantee(ctx context.Context, s *run) (string, error) {
	me, err := r.CircleCI.Me(ctx)
	if err != nil {
		return "", fmt.Errorf("the CircleCI token's user: %w", err)
	}
	if me.Login == "" {
		return "", nil
	}
	level, resp, err := r.GitHub.Repositories.GetPermissionLevel(ctx, s.owner, s.name, me.Login)
	switch {
	case isNotFound(resp, err):
		return me.Login, nil // no access at all
	case err != nil:
		return "", err
	case level.GetPermission() == permissionAdmin:
		return "", nil
	}
	return me.Login, nil
}

// stepRelease verifies the latest release: its tag has a pipeline and the
// pipeline's workflows succeeded. Either failure is a finding, never a
// rebuild: the reconciler publishes nothing a person did not ask for. A tag
// without a pipeline — the project was followed or renamed after the tag —
// is a missed build; the fix is the next tag, or the tag's pipeline
// triggered by hand. The one tag the reconciler builds is a creation's
// first release (firstRelease): the tag of the scaffold commit, which
// auto-release cuts before the project is followed, in the run whose
// circleci step follows it — the release the person asked for, built once,
// since the next run finds the pipeline the trigger started. A red
// pipeline is a release nothing was published for; the fix is a rerun of
// its failed workflow from failed — the rerun checks out the same commit
// and runs the publish jobs — or, when the cause is in the code, the next
// tag. The decision is made from the tag alone: a newer pipeline of another
// ref (the follow itself builds the default branch) is no evidence that the
// tag was built. Of every workflow name only the newest run counts: a rerun
// is a second workflow of the same name in the same pipeline, and the run
// it replaced keeps its failed status for ever. A workflow red only in
// GitHubReleaseJob is a published release: the GitHub release this step
// read is that job's one output, and every artifact was pushed before it.
//
// The release verified is one of the platform's flow, a vX.Y.Z tag
// (isReleaseTag). A latest release tagged otherwise — per component,
// base/v0.1.0 — is outside that flow: the step is skipped naming the tag,
// no pipeline is looked up and nothing is reported. A multi-image entry
// releases one line per image instead (stepImageReleases).
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
	summary, skipped, err := r.verifyRelease(ctx, s, sr, tag, release.GetCreatedAt().Time)
	if skipped {
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
// verifies a single line's. A missing CircleCI client or project skips the
// step once, for all lines alike.
func (r *Runner) stepImageReleases(ctx context.Context, s *run, sr *StepResult) error {
	releases, err := r.imageReleases(ctx, s)
	if err != nil {
		return err
	}
	if len(releases) == 0 {
		sr.Summary = "no <name>/vX.Y.Z release yet"
		return nil
	}
	var summaries []string
	for _, release := range releases {
		summary, skipped, err := r.verifyRelease(ctx, s, sr, release.GetTagName(), release.GetCreatedAt().Time)
		if err != nil {
			return err
		}
		if skipped {
			sr.Verdict = VerdictSkipped
			sr.Summary = summary
			return nil
		}
		summaries = append(summaries, summary)
	}
	sr.Summary = strings.Join(summaries, "; ")
	return nil
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

// verifyRelease verifies one release, tag, of the platform's flow, as
// stepRelease describes: findings and the first release's trigger go on sr,
// the line on the state found is returned. skipped says the release could
// not be verified at all: no CircleCI client, or the project not followed.
func (r *Runner) verifyRelease(ctx context.Context, s *run, sr *StepResult, tag string, releasedAt time.Time) (summary string, skipped bool, err error) {
	if r.CircleCI == nil {
		return fmt.Sprintf("release %s: no CircleCI client to verify the pipeline", tag), true, nil
	}

	pipeline, err := r.tagPipeline(ctx, s, tag, releasedAt)
	switch {
	case circleciclient.IsNotFound(err) && s.followedNow:
		// The follow is planned, not made (check mode): the project has no
		// pipeline yet, the tag's included.
		pipeline = nil
	case circleciclient.IsNotFound(err):
		return fmt.Sprintf("release %s: project not followed on CircleCI", tag), true, nil
	case err != nil:
		return "", false, err
	}
	if pipeline == nil {
		first, err := r.firstRelease(ctx, s, tag)
		if err != nil {
			return "", false, err
		}
		if first {
			summary := fmt.Sprintf("release %s: first release, pipeline triggered", tag)
			return summary, false, s.plan(sr, fmt.Sprintf("trigger the pipeline of %s, the first release of the created repository", tag), func() error {
				_, err := r.CircleCI.TriggerTagPipeline(ctx, s.owner, s.name, tag)
				return err
			})
		}
		s.report(sr, FindingMissedTagBuild,
			fmt.Sprintf("release %s of %s has no pipeline: nothing was built or published for the tag", tag, s.slug()),
			"cut the next tag, or trigger the tag's pipeline by hand")
		return fmt.Sprintf("release %s: no pipeline", tag), false, nil
	}

	workflows, err := r.CircleCI.ListPipelineWorkflows(ctx, pipeline.ID)
	if err != nil {
		return "", false, err
	}
	workflows = circleciclient.NewestWorkflows(workflows)
	var failed, pending, succeeded, published []string
	for _, wf := range workflows {
		switch {
		case circleciclient.WorkflowFailed(wf.Status):
			jobs, err := r.CircleCI.ListWorkflowJobs(ctx, wf.ID)
			if err != nil {
				return "", false, err
			}
			var redJobs []string
			for _, j := range jobs {
				if circleciclient.WorkflowFailed(j.Status) {
					redJobs = append(redJobs, j.Name)
				}
			}
			if unpublished := UnpublishedJobs(redJobs); len(redJobs) > 0 && len(unpublished) == 0 {
				succeeded = append(succeeded, wf.Name)
				published = append(published, fmt.Sprintf("%s (%s)", wf.Name, describe(redJobs)))
				continue
			}
			failed = append(failed, fmt.Sprintf("%s (%s: %s)", wf.Name, wf.Status, describe(redJobs)))
		case circleciclient.WorkflowSucceeded(wf.Status):
			succeeded = append(succeeded, wf.Name)
		default:
			pending = append(pending, fmt.Sprintf("%s (%s)", wf.Name, wf.Status))
		}
	}
	switch {
	case len(failed) > 0:
		summary = fmt.Sprintf("release %s: pipeline %d red", tag, pipeline.Number)
		confirm := fmt.Sprintf(", then `devctl release wait %s %s` confirms the images and chart", s.slug(), tag)
		if _, _, image := imageReleaseTag(tag); image {
			confirm = "" // release wait follows vX.Y.Z tags only
		}
		s.report(sr, FindingRedRelease,
			fmt.Sprintf("release %s of %s is red: pipeline %d, %s", tag, s.slug(), pipeline.Number, strings.Join(failed, "; ")),
			fmt.Sprintf("nothing was published for %s: rerun the failed workflow from failed on CircleCI (the rerun checks out the same commit and runs the publish jobs)%s; when the cause is in the code, fix it and let the next tag be the release", tag, confirm))
	case len(pending) > 0:
		summary = fmt.Sprintf("release %s: pipeline %d running: %s", tag, pipeline.Number, strings.Join(pending, ", "))
	case len(workflows) == 0:
		summary = fmt.Sprintf("release %s: pipeline %d has no workflows yet", tag, pipeline.Number)
	default:
		summary = fmt.Sprintf("release %s built: pipeline %d, workflows %s succeeded", tag, pipeline.Number, strings.Join(succeeded, ", "))
		if len(published) > 0 {
			summary += fmt.Sprintf("; %s failed, the GitHub release it publishes exists", strings.Join(published, ", "))
		}
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

// firstRelease says whether tag is the first release of a repository the
// platform created, missed only because the project was not followed yet:
// this run's circleci step followed the project, and the tag names the
// commit the scaffold step pushed (scaffoldSubjectPrefix). The reconciler
// never passes the change's `--added` (it validates every entry in existing
// mode), so the follow, not the request, marks the creation's run. A tag on
// a team's own commit — an adoption's first follow — and a scaffold tag of a
// project followed before stay findings.
func (r *Runner) firstRelease(ctx context.Context, s *run, tag string) (bool, error) {
	if !s.followedNow {
		return false, nil
	}
	commit, _, err := r.GitHub.Repositories.GetCommit(ctx, s.owner, s.name, tag, nil)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(commit.GetCommit().GetMessage(), scaffoldSubjectPrefix(s.name)), nil
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

// tagPipeline finds the pipeline that built tag among the project's
// pipelines, newest first, paging as far as needed to be sure: until the
// tag's pipeline is found, until a pipeline older than the release is seen
// — the tag's pipeline builds a commit no older than that, so it would have
// been listed before — or until the pages end. Nil when the tag has no
// pipeline: the build was missed.
func (r *Runner) tagPipeline(ctx context.Context, s *run, tag string, releasedAt time.Time) (*circleciclient.Pipeline, error) {
	var pageToken string
	for {
		page, err := r.CircleCI.ListPipelines(ctx, s.owner, s.name, pageToken)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			p := &page.Items[i]
			if p.VCS.Tag == tag {
				return p, nil
			}
			if p.CreatedAt.Before(releasedAt) {
				return nil, nil
			}
		}
		if page.NextPageToken == "" || len(page.Items) == 0 {
			return nil, nil
		}
		pageToken = page.NextPageToken
	}
}
