package releasewait

import (
	"context"
	"fmt"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// CircleCIReadBound bounds one CircleCI read of a poll at scale 1. The
// retrying transport sends a read that times out again for about ten
// minutes, and a CircleCI that accepts the connection and never answers
// holds the read through every try: bounded, the poll ends, the registry is
// probed and the next poll reads afresh, so a stuck read never holds the
// wait on a stale verdict until its deadline.
const CircleCIReadBound = 3 * time.Minute

// ReadTimeoutError is a CircleCI read that got no answer within
// [CircleCIReadBound]: the tag's CI went unanswered, not the release failed.
type ReadTimeoutError struct {
	Bound time.Duration
	Err   error
}

func (e *ReadTimeoutError) Error() string {
	return fmt.Sprintf("no answer within %s: %v", e.Bound, e.Err)
}

func (e *ReadTimeoutError) Unwrap() error { return e.Err }

// boundedCircleCI bounds every read of the CircleCI it stands in front of
// by [CircleCIReadBound] on the wait's clock.
type boundedCircleCI struct {
	CircleCI
	clock agentcli.Clock
}

func bounded[T any](b boundedCircleCI, ctx context.Context, read func(context.Context) (T, error)) (T, error) {
	readCtx, cancel := b.clock.Timeout(ctx, CircleCIReadBound)
	defer cancel()
	v, err := read(readCtx)
	if err != nil && readCtx.Err() != nil && ctx.Err() == nil {
		return v, &ReadTimeoutError{Bound: CircleCIReadBound, Err: err}
	}
	return v, err
}

func (b boundedCircleCI) FindPipelineByTag(ctx context.Context, org, repo, tag string) (*circleciclient.Pipeline, error) {
	return bounded(b, ctx, func(ctx context.Context) (*circleciclient.Pipeline, error) {
		return b.CircleCI.FindPipelineByTag(ctx, org, repo, tag)
	})
}

func (b boundedCircleCI) ListPipelineWorkflows(ctx context.Context, pipelineID string) ([]circleciclient.Workflow, error) {
	return bounded(b, ctx, func(ctx context.Context) ([]circleciclient.Workflow, error) {
		return b.CircleCI.ListPipelineWorkflows(ctx, pipelineID)
	})
}

func (b boundedCircleCI) ListWorkflowJobs(ctx context.Context, workflowID string) ([]circleciclient.Job, error) {
	return bounded(b, ctx, func(ctx context.Context) ([]circleciclient.Job, error) {
		return b.CircleCI.ListWorkflowJobs(ctx, workflowID)
	})
}

func (b boundedCircleCI) GetProjectJob(ctx context.Context, org, repo string, number int64) (*circleciclient.ProjectJob, error) {
	return bounded(b, ctx, func(ctx context.Context) (*circleciclient.ProjectJob, error) {
		return b.CircleCI.GetProjectJob(ctx, org, repo, number)
	})
}

func (b boundedCircleCI) GetPipeline(ctx context.Context, id string) (*circleciclient.PipelineDetail, error) {
	return bounded(b, ctx, func(ctx context.Context) (*circleciclient.PipelineDetail, error) {
		return b.CircleCI.GetPipeline(ctx, id)
	})
}

func (b boundedCircleCI) GetProjectPipeline(ctx context.Context, org, repo string, number int64) (*circleciclient.PipelineDetail, error) {
	return bounded(b, ctx, func(ctx context.Context) (*circleciclient.PipelineDetail, error) {
		return b.CircleCI.GetProjectPipeline(ctx, org, repo, number)
	})
}

func (b boundedCircleCI) FailedStepsOutput(ctx context.Context, org, repo string, number int64) (string, error) {
	return bounded(b, ctx, func(ctx context.Context) (string, error) {
		return b.CircleCI.FailedStepsOutput(ctx, org, repo, number)
	})
}
