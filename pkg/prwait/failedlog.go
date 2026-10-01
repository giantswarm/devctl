package prwait

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// DefaultFailedLogLines is how much of a failed job's log --failed-log keeps.
const DefaultFailedLogLines = 50

// The sources of a failed job.
const (
	SourceActions  = "actions"
	SourceCircleCI = "circleci"
)

// actionsApp is the app slug of the check runs GitHub Actions jobs report
// under; such a check run's id is the job's id.
const actionsApp = "github-actions"

// FailedJob is one failed job of a red head and the end of its log.
type FailedJob struct {
	Name string `json:"name"`
	// Source is actions or circleci.
	Source string `json:"source"`
	URL    string `json:"url"`
	// LogTail is the last lines of the job's log; on CircleCI, of the output
	// of its failed steps.
	LogTail string `json:"logTail,omitempty"`
	// LogError says why the log could not be read; the verdict stands.
	LogError string `json:"logError,omitempty"`
}

// actionsJob is a red check run of GitHub Actions.
type actionsJob struct {
	id        int64
	name, url string
}

// failedWorkflow is a red CircleCI workflow.
type failedWorkflow struct {
	id, name, url string
}

// FailedLog is the --failed-log flag pair of the commands that wait.
type FailedLog struct {
	Enabled bool
	Lines   int
}

// Init registers --failed-log and --failed-log-lines on cmd.
func (f *FailedLog) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.Enabled, "failed-log", false, "On a red verdict, read the log of each failed GitHub Actions and CircleCI job once: its tail goes to stderr and into failedJobs[].logTail")
	cmd.Flags().IntVar(&f.Lines, "failed-log-lines", DefaultFailedLogLines, "How many lines of each failed job's log --failed-log keeps")
}

// TailLines is the Config.FailedLogLines the flags ask for: 0 without
// --failed-log.
func (f FailedLog) TailLines() (int, error) {
	if !f.Enabled {
		return 0, nil
	}
	if f.Lines <= 0 {
		return 0, fmt.Errorf("--failed-log-lines must be positive, got %d", f.Lines)
	}
	return f.Lines, nil
}

// PrintFailedJobs writes the tail of each failed job's log to w.
func PrintFailedJobs(w io.Writer, jobs []FailedJob) {
	for _, job := range jobs {
		if job.LogError != "" {
			_, _ = fmt.Fprintf(w, "failed job %s (%s): log unreadable: %s\n", job.Name, job.URL, job.LogError)
			continue
		}
		_, _ = fmt.Fprintf(w, "failed job %s (%s):\n%s\n", job.Name, job.URL, job.LogTail)
	}
}

// failedJobs reads the log of every failed job of a red evaluation. A log
// that cannot be read is named in its job; the verdict is red either way.
func (w *Waiter) failedJobs(ctx context.Context, owner, repo string, h *head, e *evaluation) []FailedJob {
	jobs := []FailedJob{}
	for _, job := range e.failedActionsJobs {
		failed := FailedJob{Name: job.name, Source: SourceActions, URL: job.url}
		failed.LogTail, failed.LogError = w.actionsLogTail(ctx, owner, repo, job.id)
		jobs = append(jobs, failed)
	}
	for _, workflow := range e.failedWorkflows {
		jobs = append(jobs, w.circleCIFailedJobs(ctx, owner, repo, h.circleci, workflow)...)
	}
	return jobs
}

func (w *Waiter) actionsLogTail(ctx context.Context, owner, repo string, jobID int64) (string, string) {
	log, err := w.github.JobLog(ctx, owner, repo, jobID)
	if err != nil {
		return "", err.Error()
	}
	defer func() { _ = log.Close() }()
	tail, err := tailLines(log, w.failedLogLines, actionsError)
	if err != nil {
		return "", err.Error()
	}
	return tail, ""
}

func (w *Waiter) circleCIFailedJobs(ctx context.Context, owner, repo string, client *circleciclient.Client, workflow failedWorkflow) []FailedJob {
	jobs, err := client.ListWorkflowJobs(ctx, workflow.id)
	if err != nil {
		return []FailedJob{{Name: workflow.name, Source: SourceCircleCI, URL: workflow.url, LogError: err.Error()}}
	}
	var failed []FailedJob
	for _, job := range jobs {
		if !circleciclient.JobFailed(job.Status) {
			continue
		}
		f := FailedJob{
			Name:   workflow.name + "/" + job.Name,
			Source: SourceCircleCI,
			URL:    fmt.Sprintf("%s/jobs/%d", workflow.url, job.JobNumber),
		}
		output, err := client.FailedStepsOutput(ctx, owner, repo, job.JobNumber)
		if err == nil {
			f.LogTail, err = tailLines(strings.NewReader(output), w.failedLogLines, "")
		}
		if err != nil {
			f.LogError = err.Error()
		}
		failed = append(failed, f)
	}
	return failed
}

// ansiEscape matches the colour and cursor sequences CI logs carry.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// actionsError marks the line GitHub Actions logs a failed step's end with
// ("##[error]Process completed with exit code 1."); after the last one come
// the post-job steps, which do not say why the job failed.
const actionsError = "##[error]"

// tailLines returns the last n lines of r, without terminal escapes and
// carriage returns. With a non-empty until, a log that carries it ends at
// its last line containing until.
func tailLines(r io.Reader, n int, until string) (string, error) {
	var tail, untilTail []string
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = ansiEscape.ReplaceAllString(strings.TrimRight(line, "\r\n"), "")
			tail = append(tail, line)
			if len(tail) > n {
				tail = tail[1:]
			}
			if until != "" && strings.Contains(line, until) {
				untilTail = slices.Clone(tail)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if untilTail != nil {
		tail = untilTail
	}
	return strings.Join(tail, "\n"), nil
}
