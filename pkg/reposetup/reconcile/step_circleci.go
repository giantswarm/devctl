package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// stepCircleCI follows the project (under the new slug after a rename),
// enables setup workflows — the generated pipeline is dynamic and fails
// without them — and gives the project a deploy key to check out with.
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
	if len(sr.Changes) == 0 {
		sr.Summary = "followed, setup workflows on, checkout key present"
	}
	return nil
}

// permissionAdmin is GitHub's name for the administrator permission.
const permissionAdmin = "admin"

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
// pipeline is a dead tag; the fix is the next tag. The decision is made
// from the tag alone: a newer pipeline of another ref (the follow itself
// builds the default branch) is no evidence that the tag was built.
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
			fmt.Sprintf("%s is a dead tag: nothing was published for it and a rerun cannot revive it — fix the failing job's cause, merge, and let the next tag be the release; the reconciler verifies it", tag))
	case len(pending) > 0:
		sr.Summary = fmt.Sprintf("release %s: pipeline %d running: %s", tag, pipeline.Number, strings.Join(pending, ", "))
	case len(workflows) == 0:
		sr.Summary = fmt.Sprintf("release %s: pipeline %d has no workflows yet", tag, pipeline.Number)
	default:
		sr.Summary = fmt.Sprintf("release %s built: pipeline %d, workflows %s succeeded", tag, pipeline.Number, strings.Join(succeeded, ", "))
	}
	return nil
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
