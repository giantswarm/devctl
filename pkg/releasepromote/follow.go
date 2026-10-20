package releasepromote

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// DefaultFollowTimeout bounds following the dispatched runs when
// [Config.FollowTimeout] is zero: an Auto Release run decides in minutes.
const DefaultFollowTimeout = 20 * time.Minute

// followInterval is the pause between two reads of the runs still going.
const followInterval = 10 * time.Second

// failedLogLines is how much of a failed job's log a refusal keeps: the
// gate's own output ends there, before the step's error line.
const failedLogLines = 20

// The conclusions of a run that cut the release, and the run status that
// carries a conclusion.
var passedConclusions = []string{"success", "neutral", "skipped"}

const statusCompleted = "completed"

// processExit is the annotation every failed step adds after its own
// errors; it says nothing about why the step failed.
const processExit = "Process completed with exit code"

// follow reads the runs the dispatched entries started until each has
// concluded or timeout passes. A run that does not pass turns its entry
// refused, naming the run, the errors its failed jobs annotated (the stable
// gate's refusal among them) and the end of their logs in failedJobs. A run
// still going at the timeout leaves its entry dispatched with a warning.
func follow(ctx context.Context, config Config, entries []Repository) {
	var pending []int
	for i, entry := range entries {
		if entry.State == StateDispatched && entry.Run != nil {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return
	}
	timeout := config.FollowTimeout
	if timeout <= 0 {
		timeout = DefaultFollowTimeout
	}
	ctx, cancel := config.Clock.Timeout(ctx, timeout)
	defer cancel()
	for {
		pending = slices.DeleteFunc(pending, func(i int) bool {
			return followOne(ctx, config, &entries[i])
		})
		if len(pending) == 0 {
			return
		}
		if err := config.Clock.Sleep(ctx, followInterval); err != nil {
			for _, i := range pending {
				entry := &entries[i]
				warning := fmt.Sprintf("the run %s promoting %s is still %s after %s: it has not concluded; follow the release with devctl release wait %s %s", entry.Run.URL, entry.Candidate, entry.Run.Status, timeout, entry.Repository, StableVersion(entry.Candidate))
				entry.Warnings = append(entry.Warnings, warning)
				config.Progress.Printf("%s: warning: %s", entry.Repository, warning)
			}
			return
		}
	}
}

// followOne reads entry's run once and says whether it has concluded. A read
// that fails is retried at the next round.
func followOne(ctx context.Context, config Config, entry *Repository) bool {
	owner, repo, _ := SplitRepository(entry.Repository)
	run, err := config.GitHub.GetWorkflowRun(ctx, owner, repo, entry.Run.ID)
	if err != nil {
		config.Progress.Printf("%s: reading the run %s failed: %v; reading again in %s", entry.Repository, entry.Run.URL, err, followInterval)
		return false
	}
	entry.Run.Status, entry.Run.Conclusion = run.Status, run.Conclusion
	if run.URL != "" {
		entry.Run.URL = run.URL
	}
	if run.Status != statusCompleted {
		return false
	}
	if slices.Contains(passedConclusions, run.Conclusion) {
		config.Progress.Printf("%s: the run %s promoting %s concluded %s", entry.Repository, entry.Run.URL, entry.Candidate, run.Conclusion)
		return true
	}
	refuse(ctx, config, entry, owner, repo)
	config.Progress.Printf("%s: %s", entry.Repository, entry.Message)
	for _, job := range entry.FailedJobs {
		config.Progress.Printf("%s: failed job %s (%s):\n%s", entry.Repository, job.Name, job.URL, job.LogTail)
	}
	return true
}

// refuse turns entry refused by its concluded run: the errors its failed
// jobs annotated in the message, the jobs and their log tails in failedJobs.
func refuse(ctx context.Context, config Config, entry *Repository, owner, repo string) {
	reason := fmt.Sprintf("the run %s promoting %s concluded %s: no stable release is cut", entry.Run.URL, entry.Candidate, entry.Run.Conclusion)
	jobs, err := config.GitHub.ListWorkflowRunJobs(ctx, owner, repo, entry.Run.ID)
	if err != nil {
		*entry = entry.set(StateRefused, "%s; listing its jobs failed: %v", reason, err)
		return
	}
	logs := prwait.LogReader{GitHub: config.GitHub, Lines: failedLogLines}
	var errs, unread []string
	entry.FailedJobs = []prwait.FailedJob{}
	for _, job := range jobs {
		if slices.Contains(passedConclusions, job.Conclusion) || job.Conclusion == "" {
			continue
		}
		entry.FailedJobs = append(entry.FailedJobs, logs.ActionsJob(ctx, owner, repo, job.ID, job.Name, job.URL))
		annotations, err := config.GitHub.CheckRunAnnotations(ctx, owner, repo, job.ID)
		if err != nil {
			unread = append(unread, fmt.Sprintf("the annotations of job %s: %v", job.Name, err))
			continue
		}
		errs = append(errs, jobErrors(annotations)...)
	}
	switch {
	case len(errs) > 0:
		reason = fmt.Sprintf("%s; %s", reason, strings.Join(errs, "; "))
	case len(unread) == 0:
		reason += "; its failed jobs annotated no error: see failedJobs"
	}
	if len(unread) > 0 {
		reason = fmt.Sprintf("%s; reading %s failed", reason, strings.Join(unread, ", "))
	}
	*entry = entry.set(StateRefused, "%s", reason)
}

// jobErrors are the failure annotations of a job, "title: message" each, but
// the exit code every failed step adds.
func jobErrors(annotations []githubclient.Annotation) []string {
	var errs []string
	for _, a := range annotations {
		if a.Level != "failure" || strings.HasPrefix(a.Message, processExit) {
			continue
		}
		if a.Title == "" {
			errs = append(errs, a.Message)
			continue
		}
		errs = append(errs, a.Title+": "+a.Message)
	}
	return errs
}

// StableVersion is the stable release a candidate vX.Y.Z-rc.N promotes to,
// vX.Y.Z.
func StableVersion(candidate string) string {
	version, _, _ := strings.Cut(candidate, "-rc.")
	return version
}
