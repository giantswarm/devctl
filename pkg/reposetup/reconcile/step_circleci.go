package reconcile

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
)

// stepCircleCI follows the project (under the new slug after a rename),
// enables setup workflows — the generated pipeline is dynamic and fails
// without them — gives the project a deploy key to check out with, and
// verifies the webhook CircleCI installs on the follow: without it no push
// and no tag reaches CircleCI, and the project is followed in name only.
func (r *Runner) stepCircleCI(ctx context.Context, s *run, sr *StepResult) error {
	if r.CircleCI == nil {
		sr.Verdict = VerdictSkipped
		sr.Summary = "no CircleCI client"
		return nil
	}
	followed, err := r.circleCIFollowed(ctx, s)
	if err != nil {
		return err
	}
	if !followed {
		change := "follow " + s.slug()
		if s.renamed {
			change = fmt.Sprintf("follow %s under its new slug (declared as %s)", s.slug(), s.declared)
		}
		grantee, err := r.followGrantee(ctx, s)
		if err != nil {
			return err
		}
		if grantee != "" {
			grant := fmt.Sprintf("grant %s admin for the CircleCI follow, revoked after it", grantee)
			err := s.plan(sr, grant, func() error {
				_, _, err := r.GitHub.Repositories.AddCollaborator(ctx, s.owner, s.name, grantee, &github.RepositoryAddCollaboratorOptions{Permission: permissionAdmin})
				return err
			})
			if err != nil {
				return err
			}
		}
		err = s.plan(sr, change, func() error {
			err := r.CircleCI.Follow(ctx, s.owner, s.name)
			if grantee != "" {
				// The grant is for the follow alone, kept or not.
				if _, rerr := r.GitHub.Repositories.RemoveCollaborator(ctx, s.owner, s.name, grantee); rerr != nil {
					err = errors.Join(err, fmt.Errorf("revoke the admin grant of %s: %w", grantee, rerr))
				}
			}
			return err
		})
		if err != nil {
			return err
		}
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
	if len(keys) == 0 {
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
		sr.Summary = "followed, setup workflows on, checkout key present, " + webhook
	}
	return nil
}

// circleCIWebhookURL is the URL of the webhook CircleCI installs on a
// repository it follows: every push and tag reaches CircleCI through it.
const circleCIWebhookURL = "https://circleci.com/hooks/github"

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
	for _, h := range hooks {
		if h.GetConfig().GetURL() == circleCIWebhookURL && h.GetActive() && slices.Contains(h.Events, "push") {
			return "webhook present", nil
		}
	}
	s.report(sr, FindingCircleCIWebhookMissing,
		fmt.Sprintf("%s is followed on CircleCI but carries no active CircleCI webhook (%s, push events): no push and no tag reaches CircleCI, so no branch builds and the first release tag goes unbuilt", s.slug(), circleCIWebhookURL),
		fmt.Sprintf("CircleCI installs its webhook only for a follow by a GitHub admin of the repository whose CircleCI grant carries the hook scope: follow the project as such a user (POST /api/v1.1/project/github/%s/follow) or through Project Settings on CircleCI; devctl cannot create the hook, CircleCI signs it with its own secret", s.slug()))
	return "webhook missing", nil
}

// permissionAdmin is GitHub's name for the administrator permission.
const permissionAdmin = "admin"

// circleCIConfig is the pipeline's entry point on the default branch.
const circleCIConfig = ".circleci/config.yml"

// hasPipeline says whether the repository has a CircleCI pipeline for the
// circleci and release steps to act on: the entry declares a generated one
// (gen.ci.generate: true — on a first creation the config is not on the
// branch yet), or .circleci/config.yml is on the default branch. The field
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
	if g := s.fields.Gen; g != nil && g.CI != nil && g.CI.Generate != nil && *g.CI.Generate {
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

// followGrantee returns the login of the CircleCI token's GitHub user when
// that user is not an administrator of the repository: CircleCI follows a
// project for a repository administrator only ("only a project's Github
// administrator may setup Circle"), and the reconciler's identity holds push
// through the bots team. Empty when no grant is needed.
func (r *Runner) followGrantee(ctx context.Context, s *run) (string, error) {
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
// rebuild: the reconciler publishes nothing. A tag without a pipeline —
// the project was followed or renamed after the tag — is a missed build;
// the fix is the next tag, or the tag's pipeline triggered by hand. A red
// pipeline is a release nothing was published for; the fix is a rerun of
// its failed workflow from failed — the rerun checks out the same commit
// and runs the publish jobs — or, when the cause is in the code, the next
// tag. The decision is made from the tag alone: a newer pipeline of another
// ref (the follow itself builds the default branch) is no evidence that the
// tag was built. Of every workflow name only the newest run counts: a rerun
// is a second workflow of the same name in the same pipeline, and the run
// it replaced keeps its failed status for ever.
//
// The release verified is one of the platform's flow, a vX.Y.Z tag
// (isReleaseTag). A latest release tagged otherwise — per component,
// base/v0.1.0 — is outside that flow: the step is skipped naming the tag,
// no pipeline is looked up and nothing is reported.
func (r *Runner) stepRelease(ctx context.Context, s *run, sr *StepResult) error {
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
	if r.CircleCI == nil {
		sr.Verdict = VerdictSkipped
		sr.Summary = fmt.Sprintf("release %s: no CircleCI client to verify the pipeline", tag)
		return nil
	}

	pipeline, err := r.tagPipeline(ctx, s, tag, release.GetCreatedAt().Time)
	if err != nil {
		if circleciclient.IsNotFound(err) {
			sr.Verdict = VerdictSkipped
			sr.Summary = fmt.Sprintf("release %s: project not followed on CircleCI", tag)
			return nil
		}
		return err
	}
	if pipeline == nil {
		s.report(sr, FindingMissedTagBuild,
			fmt.Sprintf("release %s of %s has no pipeline: nothing was built or published for the tag", tag, s.slug()),
			"cut the next tag, or trigger the tag's pipeline by hand")
		return nil
	}

	workflows, err := r.CircleCI.ListPipelineWorkflows(ctx, pipeline.ID)
	if err != nil {
		return err
	}
	workflows = circleciclient.NewestWorkflows(workflows)
	var failed, pending, succeeded []string
	for _, wf := range workflows {
		switch {
		case circleciclient.WorkflowFailed(wf.Status):
			jobs, err := r.CircleCI.ListWorkflowJobs(ctx, wf.ID)
			if err != nil {
				return err
			}
			var redJobs []string
			for _, j := range jobs {
				if circleciclient.WorkflowFailed(j.Status) {
					redJobs = append(redJobs, j.Name)
				}
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
		s.report(sr, FindingRedRelease,
			fmt.Sprintf("release %s of %s is red: pipeline %d, %s", tag, s.slug(), pipeline.Number, strings.Join(failed, "; ")),
			fmt.Sprintf("nothing was published for %s: rerun the failed workflow from failed on CircleCI (the rerun checks out the same commit and runs the publish jobs), then `devctl release wait %s %s` confirms the images and chart; when the cause is in the code, fix it and let the next tag be the release", tag, s.slug(), tag))
	case len(pending) > 0:
		sr.Summary = fmt.Sprintf("release %s: pipeline %d running: %s", tag, pipeline.Number, strings.Join(pending, ", "))
	case len(workflows) == 0:
		sr.Summary = fmt.Sprintf("release %s: pipeline %d has no workflows yet", tag, pipeline.Number)
	default:
		sr.Summary = fmt.Sprintf("release %s built: pipeline %d, workflows %s succeeded", tag, pipeline.Number, strings.Join(succeeded, ", "))
	}
	return nil
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
