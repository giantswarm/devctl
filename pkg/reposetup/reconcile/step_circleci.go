package reconcile

import (
	"context"
	"fmt"
	"strings"

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
		if err := s.plan(sr, change, func() error { return r.CircleCI.Follow(ctx, s.owner, s.name) }); err != nil {
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

// stepRelease verifies the latest release: its tag has a pipeline and the
// pipeline's workflows succeeded. A tag without a pipeline — the project
// was followed or renamed after the tag — is triggered; a red pipeline is
// reported: the tag is dead and the fix is the next tag.
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

	pipelines, err := r.CircleCI.ListPipelines(ctx, s.owner, s.name)
	if err != nil {
		if circleciclient.IsNotFound(err) {
			sr.Verdict = VerdictSkipped
			sr.Summary = fmt.Sprintf("release %s: project not followed on CircleCI", tag)
			return nil
		}
		return err
	}
	var pipeline *circleciclient.Pipeline
	for i := range pipelines {
		if pipelines[i].VCS.Tag == tag {
			pipeline = &pipelines[i]
			break
		}
	}
	if pipeline == nil {
		// The list is the first page, newest first. The tag's pipeline is
		// missing for sure only when the page reaches back past the release.
		if n := len(pipelines); n > 0 && pipelines[n-1].CreatedAt.After(release.GetCreatedAt().Time) {
			sr.Summary = fmt.Sprintf("release %s: pipeline not among the %d most recent, not verified", tag, n)
			return nil
		}
		return s.plan(sr, fmt.Sprintf("trigger the missed tag build for %s", tag), func() error {
			_, err := r.CircleCI.TriggerPipeline(ctx, s.owner, s.name, circleciclient.TriggerRequest{Tag: tag})
			return err
		})
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
