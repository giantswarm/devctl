package releasewait

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// statusPipelines resolves the tag pipeline from the CircleCI statuses of
// the tag's commit when CircleCI's pipeline listing does not name it: the
// listing's newest pages hold another branch's pipelines on a busy
// repository, while the statuses link the jobs that built the tag.
type statusPipelines struct {
	// pipeline is the tag pipeline once a status led to it.
	pipeline *circleciclient.Pipeline
	// jobs and pipelines are the job and pipeline numbers read already,
	// each read once: a status of the branch's pipeline on the same commit
	// leads to a pipeline that did not build the tag, for good.
	jobs, pipelines map[int64]bool
	// foreign are the target URLs that led to a pipeline of another tag or
	// a branch: their statuses say nothing about the tag's CI.
	foreign map[string]bool
	// warned says the document carries the warning that the statuses are
	// the pipeline's verdict.
	warned bool
}

// pipelineFromStatuses returns the tag pipeline a CircleCI status of the
// tag's commit links, nil when none does, and the CircleCI statuses that
// may be the tag pipeline's: the latest of every ci/circleci context, but
// for those that led to another pipeline.
func (w *Waiter) pipelineFromStatuses(ctx context.Context, result *Result) (*circleciclient.Pipeline, []githubclient.CommitStatus, error) {
	sp := &w.statusPipelines
	if sp.pipeline != nil {
		return sp.pipeline, nil, nil
	}
	owner, repo := w.config.Owner, w.config.Repo
	all, err := w.config.GitHub.ListCommitStatuses(ctx, owner, repo, result.SHA)
	if githubclient.IsNotFound(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading the commit statuses of %s: %w", short(result.SHA), err)
	}
	if sp.jobs == nil {
		sp.jobs, sp.pipelines, sp.foreign = map[int64]bool{}, map[int64]bool{}, map[string]bool{}
	}
	var circleci []githubclient.CommitStatus
	for _, s := range all {
		if strings.HasPrefix(s.Context, circleciclient.StatusContextPrefix) {
			circleci = append(circleci, s)
		}
	}
	for _, s := range circleci {
		target, ok := circleciclient.ParseStatusTarget(s.TargetURL)
		if !ok || !strings.EqualFold(target.Org, owner) || !strings.EqualFold(target.Repo, repo) {
			continue
		}
		var detail *circleciclient.PipelineDetail
		switch {
		case target.Pipeline > 0 && !sp.pipelines[target.Pipeline]:
			sp.pipelines[target.Pipeline] = true
			detail, err = w.circleci.GetProjectPipeline(ctx, owner, repo, target.Pipeline)
		case target.Pipeline == 0 && target.Job > 0 && !sp.jobs[target.Job]:
			sp.jobs[target.Job] = true
			var job *circleciclient.ProjectJob
			job, err = w.circleci.GetProjectJob(ctx, owner, repo, target.Job)
			if err == nil && job.Pipeline.ID != "" {
				detail, err = w.circleci.GetPipeline(ctx, job.Pipeline.ID)
			}
		default:
			continue
		}
		switch {
		case circleciclient.IsNotFound(err):
			continue
		case err != nil:
			return nil, nil, fmt.Errorf("reading the pipeline the status %s of %s links: %w", s.Context, short(result.SHA), err)
		case detail == nil:
			continue
		}
		if detail.VCS.Tag != result.Tag {
			sp.foreign[s.TargetURL] = true
			continue
		}
		w.progress.Printf("CircleCI's pipeline listing does not name %s; its commit's status %s links pipeline %d", result.Tag, s.Context, detail.Number)
		sp.pipeline = &detail.Pipeline
		return sp.pipeline, nil, nil
	}
	var statuses []githubclient.CommitStatus
	for _, s := range circleci {
		if !sp.foreign[s.TargetURL] {
			statuses = append(statuses, s)
		}
	}
	return nil, statuses, nil
}

// statusesState is the tag's CI read from the CircleCI statuses of its
// commit, for when no status leads to a pipeline CircleCI answers for: a
// failed status fails it, a pending one keeps it running, and it is green
// when every status succeeded. The job names are the statuses' contexts.
func (w *Waiter) statusesState(result *Result, statuses []githubclient.CommitStatus) *pipelineState {
	if !w.statusPipelines.warned {
		w.statusPipelines.warned = true
		w.warn(fmt.Sprintf("CircleCI names no pipeline for %s, neither in its pipeline listing nor behind the commit statuses of %s; the tag's CI verdict is those %d CircleCI statuses", result.Tag, short(result.SHA), len(statuses)))
	}
	state := &pipelineState{green: true, jobs: map[string]bool{}}
	for _, s := range statuses {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s.Context, circleciclient.StatusContextPrefix), ":"))
		state.jobs[name] = true
		switch s.State {
		case "success":
		case "failure", "error":
			state.failed = true
			state.green = false
			state.failedJobs = append(state.failedJobs, name)
		default:
			state.green = false
		}
		w.progress.Printf("status %s: %s", s.Context, s.State)
	}
	sort.Strings(state.failedJobs)
	return state
}
