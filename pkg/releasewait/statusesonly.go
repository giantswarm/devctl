package releasewait

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/circleciconfig"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// listCircleJobs lists, for a wait with Config.CircleCIFromStatuses, the
// jobs the tag's CircleCI configuration runs on the tag: the statuses the
// tag's CI waits for.
func (w *Waiter) listCircleJobs(ctx context.Context, result *Result) error {
	owner, repo := w.config.Owner, w.config.Repo
	read := func(ctx context.Context, path string) ([]byte, bool, error) {
		file, err := w.config.GitHub.GetFile(ctx, owner, repo, path, result.SHA)
		if githubclient.IsNotFound(err) {
			return nil, false, nil
		}
		return file.Data, err == nil, err
	}
	jobs, _, err := circleciconfig.List(ctx, read, circleciconfig.Run{Tag: result.Tag})
	if err != nil {
		return fmt.Errorf("listing the CircleCI jobs of %s: %w", result.Tag, err)
	}
	for _, warning := range jobs.Warnings {
		w.warn(warning)
	}
	w.circleJobs = jobs.Names
	w.progress.Printf("CircleCI through its commit statuses: %s runs %s", result.Tag, strings.Join(jobs.Names, ", "))
	return nil
}

// circleStatusesState is the tag's CI read from the CircleCI statuses of its
// commit alone, for a wait without a CircleCI client. It is green once every
// job the tag's configuration runs has posted a status, no job (each by the
// job its status links) is still pending, and the newest status of no
// context failed; a failed one fails it. A job the branch's pipeline runs as
// well may have posted from that pipeline; the tag pipeline's own jobs
// (the push jobs, filtered to tags) require the jobs before them, so they
// post only once those ran in the tag pipeline.
func (w *Waiter) circleStatusesState(ctx context.Context, result *Result) (*pipelineState, error) {
	all, err := w.config.GitHub.ListCommitStatuses(ctx, w.config.Owner, w.config.Repo, result.SHA)
	if githubclient.IsNotFound(err) {
		all = nil
	} else if err != nil {
		return nil, fmt.Errorf("reading the commit statuses of %s: %w", short(result.SHA), err)
	}
	// The statuses come newest first: the first of a context or a job is
	// its newest.
	byContext := map[string]githubclient.CommitStatus{}
	byJob := map[string]githubclient.CommitStatus{}
	for _, s := range all {
		if !strings.HasPrefix(s.Context, circleciclient.StatusContextPrefix) {
			continue
		}
		if _, ok := byContext[s.Context]; !ok {
			byContext[s.Context] = s
		}
		if _, ok := byJob[s.TargetURL]; !ok {
			byJob[s.TargetURL] = s
		}
	}

	doc := &Pipeline{Workflows: []PipelineWorkflow{}, FailedJobs: []string{}, Unfinished: []string{}}
	state := &pipelineState{green: true, jobs: map[string]bool{}}
	contexts := make([]string, 0, len(byContext))
	for c := range byContext {
		contexts = append(contexts, c)
	}
	sort.Strings(contexts)
	for _, c := range contexts {
		s := byContext[c]
		name := circleciclient.StatusJobName(c)
		state.jobs[name] = true
		doc.Workflows = append(doc.Workflows, PipelineWorkflow{Name: name, Status: s.State})
		switch s.State {
		case commitStatusFailure, commitStatusError:
			if circleciclient.AdvisoryJob(name) {
				w.warnOnce(fmt.Sprintf("the commit status %s of %s is %s; the release does not require it, so the tag's CI is judged without it", c, short(result.SHA), s.State))
				continue
			}
			state.failed, state.green = true, false
			doc.FailedJobs = append(doc.FailedJobs, name)
		}
		w.progress.Printf("status %s: %s", c, s.State)
	}
	var pending []string
	for _, s := range byJob {
		if s.State != commitStatusSuccess && s.State != commitStatusFailure && s.State != commitStatusError {
			pending = append(pending, fmt.Sprintf("%s (%s)", circleciclient.StatusJobName(s.Context), s.State))
		}
	}
	sort.Strings(pending)
	doc.Unfinished = append(doc.Unfinished, pending...)
	if len(byContext) == 0 {
		doc.Unfinished = append(doc.Unfinished, "no CircleCI status on "+short(result.SHA)+" yet")
	}
	posted := true
	for _, job := range w.circleJobs {
		if !state.jobs[job] {
			posted = false
			doc.Unfinished = append(doc.Unfinished, job+" (not posted yet)")
		}
	}
	if len(doc.Unfinished) > 0 {
		state.green = false
	}
	if !posted {
		// The jobs are known once every job the configuration runs posted.
		state.jobs = nil
	}
	sort.Strings(doc.FailedJobs)
	state.failedJobs = doc.FailedJobs
	result.Pipeline = doc
	return state, nil
}
