package ci

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// DefaultJobLogLines is how many lines of a job's failed steps' output
// `ci job` keeps.
const DefaultJobLogLines = 200

// JobResult is the document part of `ci job`.
type JobResult struct {
	Repository string `json:"repository"`
	// Job is absent when CircleCI has no such job.
	Job *JobDetail `json:"job,omitempty"`
}

// JobDetail is one job with its steps and the end of its failed steps'
// output.
type JobDetail struct {
	Number     int64      `json:"number"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	URL        string     `json:"url,omitempty"`
	PipelineID string     `json:"pipelineId,omitempty"`
	WorkflowID string     `json:"workflowId,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	StoppedAt  *time.Time `json:"stoppedAt,omitempty"`
	Steps      []JobStep  `json:"steps"`
	// LogTail is the last lines of the output of the job's failed steps, led
	// by the lines from its first failure when that lies before them; empty
	// when no step failed.
	LogTail string `json:"logTail,omitempty"`
	// LogError says why that output could not be read; the rest stands.
	LogError string `json:"logError,omitempty"`
}

// JobStep is one step of the job, a parallel step once per action.
type JobStep struct {
	Name      string     `json:"name"`
	Index     int        `json:"index"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// NewJobResult is an empty result for repository.
func NewJobResult(repository string) *JobResult {
	return &JobResult{Repository: repository}
}

// ReadJob reads job number ref of org/repo into result: its status, its steps
// and the tail of its failed steps' output, lines of it. Exit 3 when
// CircleCI has no such job of the repository. Steps CircleCI does not answer
// are a warning; output it does not answer is the job's LogError.
func ReadJob(ctx context.Context, client *circleciclient.Client, org, repo, ref string, lines int, result *JobResult, warn func(string)) error {
	number, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || number <= 0 {
		return fmt.Errorf("the job number is a positive integer, got %q", ref)
	}
	job, err := client.GetProjectJob(ctx, org, repo, number)
	switch {
	case circleciclient.IsNotFound(err):
		return notApplicable("CircleCI has no job %d of %s/%s the login sees", number, org, repo)
	case err != nil:
		return err
	}
	detail := &JobDetail{
		Number:     job.Number,
		Name:       job.Name,
		Status:     job.Status,
		URL:        job.WebURL,
		PipelineID: job.Pipeline.ID,
		WorkflowID: job.LatestWorkflow.ID,
		StartedAt:  timePtr(job.StartedAt),
		StoppedAt:  timePtr(job.StoppedAt),
		Steps:      []JobStep{},
	}
	result.Job = detail

	steps, err := client.JobSteps(ctx, org, repo, number)
	if err != nil {
		warn(fmt.Sprintf("the steps of job %s (%d) could not be read: %v", job.Name, number, err))
	}
	for _, s := range steps {
		detail.Steps = append(detail.Steps, JobStep{Name: s.Name, Index: s.Index, Status: s.Status, StartedAt: timePtr(s.StartedAt), EndedAt: timePtr(s.EndedAt)})
	}

	logs := prwait.LogReader{Lines: lines}.CircleCIJob(ctx, org, repo, client, number, job.Name, job.WebURL)
	detail.LogTail, detail.LogError = logs.LogTail, logs.LogError
	if detail.LogTail == "" && detail.LogError == "" && circleciclient.JobFailed(job.Status) {
		warn(fmt.Sprintf("job %s (%d) reads %s, but no step of it failed with output", job.Name, number, job.Status))
	}
	return nil
}
