package ci

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// jobRunning is the status of a job that is executing right now, the one
// whose steps say whether it is slow or stuck.
const jobRunning = "running"

// JobsResult is the document part of `ci jobs`.
type JobsResult struct {
	Repository string `json:"repository"`
	// Pipeline is absent when CircleCI has no such pipeline.
	Pipeline  *Pipeline  `json:"pipeline,omitempty"`
	Workflows []Workflow `json:"workflows"`
}

// Workflow is one workflow run of the pipeline with its jobs. Every run is
// listed, reruns included, so what was rerun and when is visible.
type Workflow struct {
	Run
	// Latest says whether this is the newest run of its name in the
	// pipeline, the one that says where the pipeline stands; an older run
	// is a rerun's predecessor.
	Latest bool  `json:"latest"`
	Jobs   []Job `json:"jobs"`
}

// Job is one job of a workflow.
type Job struct {
	Name string `json:"name"`
	// Number is the job's number in the project; absent for an approval and
	// for a job that has not started.
	Number    int64      `json:"number,omitempty"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	StoppedAt *time.Time `json:"stoppedAt,omitempty"`
	// DurationSeconds is how long the job ran, or has been running.
	DurationSeconds int64 `json:"durationSeconds,omitempty"`
	// Step is where a running job stands; absent for every other job, and
	// for a running job whose steps CircleCI did not answer (a warning says
	// so).
	Step *Step `json:"step,omitempty"`
}

// Step is the step a running job is in: the last one that started.
type Step struct {
	Name      string     `json:"name"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// EndedAt is set when the step finished and the job is between steps.
	EndedAt *time.Time `json:"endedAt,omitempty"`
	// RunningSeconds is how long the step has been running.
	RunningSeconds int64 `json:"runningSeconds"`
	// LastOutputAt is when the step last wrote a line; absent when it has
	// written nothing yet.
	LastOutputAt *time.Time `json:"lastOutputAt,omitempty"`
	// OutputAgeSeconds is how long ago that was: seconds for a job that is
	// slow, an hour for one that is stuck. 0 without LastOutputAt.
	OutputAgeSeconds int64 `json:"outputAgeSeconds"`
}

// NewJobsResult is an empty result for repository.
func NewJobsResult(repository string) *JobsResult {
	return &JobsResult{Repository: repository, Workflows: []Workflow{}}
}

// Jobs reads pipeline, a number or an id, of org/repo at the job level into
// result as of now: every workflow run with its jobs, and for a running job
// the step it is in and when that step last wrote output. Exit 3 when
// CircleCI has no such pipeline of the repository. Jobs or steps CircleCI
// does not answer are a warning, not an outcome: the rest of the view still
// tells where the pipeline stands.
func Jobs(ctx context.Context, client *circleciclient.Client, org, repo, pipeline string, now time.Time, result *JobsResult, warn func(string)) error {
	detail, err := findPipeline(ctx, client, org, repo, pipeline)
	if err != nil {
		return err
	}
	result.Pipeline = newPipeline(org, repo, detail.Pipeline)

	runs, err := client.ListPipelineWorkflows(ctx, detail.ID)
	if err != nil {
		return err
	}
	latest := map[string]string{}
	for _, run := range circleciclient.NewestWorkflows(runs) {
		latest[run.Name] = run.ID
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].Name != runs[j].Name {
			return runs[i].Name < runs[j].Name
		}
		return runs[i].CreatedAt.Before(runs[j].CreatedAt)
	})
	for _, run := range runs {
		w := Workflow{Run: newRun(org, repo, detail.Number, run), Latest: latest[run.Name] == run.ID, Jobs: []Job{}}
		jobs, err := client.ListWorkflowJobs(ctx, run.ID)
		switch {
		case circleciclient.IsNotFound(err):
			warn(fmt.Sprintf("the jobs of workflow %s (%s) are not listed yet", run.Name, run.ID))
		case err != nil:
			return err
		}
		for _, job := range jobs {
			w.Jobs = append(w.Jobs, newJob(ctx, client, org, repo, job, now, warn))
		}
		result.Workflows = append(result.Workflows, w)
	}
	return nil
}

// findPipeline resolves ref, the pipeline's number in the project or its id,
// to the pipeline, which has to belong to org/repo.
func findPipeline(ctx context.Context, client *circleciclient.Client, org, repo, ref string) (*circleciclient.PipelineDetail, error) {
	var detail *circleciclient.PipelineDetail
	var err error
	if number, ok := pipelineNumber(ref); ok {
		detail, err = client.GetProjectPipeline(ctx, org, repo, number)
	} else {
		detail, err = client.GetPipeline(ctx, ref)
	}
	switch {
	case circleciclient.IsNotFound(err):
		return nil, notApplicable("CircleCI has no pipeline %s of %s/%s the login sees", ref, org, repo)
	case err != nil:
		return nil, err
	}
	if !sameProject(detail.ProjectSlug, org, repo) {
		return nil, notApplicable("pipeline %s belongs to %s, not %s/%s", ref, detail.ProjectSlug, org, repo)
	}
	return detail, nil
}

// pipelineNumber reads ref as a pipeline number; anything else is an id.
func pipelineNumber(ref string) (int64, bool) {
	n, err := strconv.ParseInt(ref, 10, 64)
	return n, err == nil && n > 0
}

func newJob(ctx context.Context, client *circleciclient.Client, org, repo string, job circleciclient.Job, now time.Time, warn func(string)) Job {
	j := Job{
		Name:      job.Name,
		Number:    job.JobNumber,
		Type:      job.Type,
		Status:    job.Status,
		StartedAt: timePtr(job.StartedAt),
		StoppedAt: timePtr(job.StoppedAt),
	}
	switch {
	case job.StartedAt.IsZero():
	case job.StoppedAt.IsZero():
		j.DurationSeconds = seconds(now.Sub(job.StartedAt))
	default:
		j.DurationSeconds = seconds(job.StoppedAt.Sub(job.StartedAt))
	}
	if job.Status == jobRunning && job.JobNumber > 0 {
		j.Step = currentStep(ctx, client, org, repo, job, now, warn)
	}
	return j
}

// currentStep is the step a running job is in, the last one that started,
// with the age of its last output line. Steps or output CircleCI does not
// answer are a warning and no step.
func currentStep(ctx context.Context, client *circleciclient.Client, org, repo string, job circleciclient.Job, now time.Time, warn func(string)) *Step {
	steps, err := client.JobSteps(ctx, org, repo, job.JobNumber)
	if err != nil {
		warn(fmt.Sprintf("the steps of job %s (%d) could not be read: %v", job.Name, job.JobNumber, err))
		return nil
	}
	var current *circleciclient.Step
	for i := range steps {
		if steps[i].StartedAt.IsZero() {
			continue
		}
		if current == nil || steps[i].StartedAt.After(current.StartedAt) {
			current = &steps[i]
		}
	}
	if current == nil {
		return nil
	}
	s := &Step{Name: current.Name, StartedAt: timePtr(current.StartedAt), EndedAt: timePtr(current.EndedAt)}
	if current.EndedAt.IsZero() {
		s.RunningSeconds = seconds(now.Sub(current.StartedAt))
	} else {
		s.RunningSeconds = seconds(current.EndedAt.Sub(current.StartedAt))
	}
	if current.OutputURL == "" {
		return s
	}
	last, err := client.LastOutputAt(ctx, current.OutputURL)
	if err != nil {
		warn(fmt.Sprintf("the output of step %q of job %s (%d) could not be read: %v", current.Name, job.Name, job.JobNumber, err))
		return s
	}
	if !last.IsZero() {
		s.LastOutputAt = timePtr(last)
		s.OutputAgeSeconds = seconds(now.Sub(last))
	}
	return s
}
