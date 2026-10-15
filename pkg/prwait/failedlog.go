package prwait

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"

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
	logs := LogReader{GitHub: w.github, Lines: w.failedLogLines}
	jobs := []FailedJob{}
	for _, job := range e.failedActionsJobs {
		jobs = append(jobs, logs.ActionsJob(ctx, owner, repo, job.id, job.name, job.url))
	}
	for _, workflow := range e.failedWorkflows {
		jobs = append(jobs, logs.CircleCIWorkflow(ctx, owner, repo, h.circleci, workflow.id, workflow.name, workflow.url)...)
	}
	return jobs
}

// JobLogs reads the log of a GitHub Actions job.
type JobLogs interface {
	JobLog(ctx context.Context, owner, repo string, jobID int64) (io.ReadCloser, error)
}

// CircleCIJobs lists the jobs of a CircleCI workflow and reads the output of
// a job's failed steps.
type CircleCIJobs interface {
	ListWorkflowJobs(ctx context.Context, workflowID string) ([]circleciclient.Job, error)
	FailedStepsOutput(ctx context.Context, org, repo string, number int64) (string, error)
}

// LogReader reads the tails of failed jobs' logs for the commands that wait
// with --failed-log: GitHub Actions job logs and the output of CircleCI's
// failed steps, without terminal control sequences or control characters but tab. A log that cannot be read is named
// in the job's LogError.
type LogReader struct {
	GitHub JobLogs
	// Lines is how many of a log's last lines a job keeps.
	Lines int
}

// ActionsJob is the failed GitHub Actions job id with the tail of its log; a
// log that ends with a failed step's error ends at that error.
func (r LogReader) ActionsJob(ctx context.Context, owner, repo string, id int64, name, url string) FailedJob {
	job := FailedJob{Name: name, Source: SourceActions, URL: url}
	log, err := r.GitHub.JobLog(ctx, owner, repo, id)
	if err != nil {
		job.LogError = err.Error()
		return job
	}
	defer func() { _ = log.Close() }()
	if job.LogTail, err = tailLines(log, r.Lines, actionsError); err != nil {
		job.LogError = err.Error()
	}
	return job
}

// CircleCIWorkflow is every failed job of the failed CircleCI workflow id,
// named workflow/job, with the tail of its failed steps' output; the
// workflow itself when its jobs cannot be listed.
func (r LogReader) CircleCIWorkflow(ctx context.Context, owner, repo string, client CircleCIJobs, id, name, url string) []FailedJob {
	jobs, err := client.ListWorkflowJobs(ctx, id)
	if err != nil {
		return []FailedJob{{Name: name, Source: SourceCircleCI, URL: url, LogError: err.Error()}}
	}
	var failed []FailedJob
	for _, job := range jobs {
		if !circleciclient.JobFailed(job.Status) {
			continue
		}
		failed = append(failed, r.CircleCIJob(ctx, owner, repo, client, job.JobNumber, name+"/"+job.Name, fmt.Sprintf("%s/jobs/%d", url, job.JobNumber)))
	}
	return failed
}

// CircleCIJob is the failed CircleCI job number with the tail of its failed
// steps' output.
func (r LogReader) CircleCIJob(ctx context.Context, owner, repo string, client CircleCIJobs, number int64, name, url string) FailedJob {
	job := FailedJob{Name: name, Source: SourceCircleCI, URL: url}
	output, err := client.FailedStepsOutput(ctx, owner, repo, number)
	if err == nil {
		job.LogTail, err = tailLines(strings.NewReader(output), r.Lines, "")
	}
	if err != nil {
		job.LogError = err.Error()
	}
	return job
}

// actionsError marks the line GitHub Actions logs a failed step's end with
// ("##[error]Process completed with exit code 1."); after the last one come
// the post-job steps, which do not say why the job failed.
const actionsError = "##[error]"

// tailLines returns the last n lines of r, without terminal control
// sequences and control characters (stripControls). With a non-empty until, a log that carries it ends at
// its last line containing until.
func tailLines(r io.Reader, n int, until string) (string, error) {
	var tail, untilTail []string
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			line = stripControls(line)
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

// stripControls returns line without its terminal control sequences and
// control characters: a CI log is written by the code under test, and its
// tail reaches a terminal and a reader that trusts it. Every escape sequence
// goes whole, its payload included: CSI, OSC up to BEL or ST, DCS, SOS, PM
// and APC strings up to ST, and the other ESC sequences, in their 7-bit and
// 8-bit (C1) forms. Every other C0 and C1 control goes too, except tab,
// and the bidirectional controls, which reorder what a reader sees.
func stripControls(line string) string {
	runes := []rune(line)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\x1b' && i+1 < len(runes) {
			switch next := runes[i+1]; {
			case next == '[':
				i = skipCSI(runes, i+2)
				continue
			case next == ']':
				i = skipString(runes, i+2, true)
				continue
			case next == 'P' || next == 'X' || next == '^' || next == '_':
				i = skipString(runes, i+2, false)
				continue
			case next >= 0x20 && next <= 0x7e:
				i = skipEscape(runes, i+1)
				continue
			}
		}
		switch r {
		case 0x9b:
			i = skipCSI(runes, i+1)
			continue
		case 0x9d:
			i = skipString(runes, i+1, true)
			continue
		case 0x90, 0x98, 0x9e, 0x9f:
			i = skipString(runes, i+1, false)
			continue
		}
		if r != '\t' && unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// skipCSI returns the index of the final byte of the control sequence whose
// parameters start at i, or the last index when it has none.
func skipCSI(runes []rune, i int) int {
	for ; i < len(runes); i++ {
		if runes[i] >= 0x40 && runes[i] <= 0x7e {
			return i
		}
	}
	return len(runes) - 1
}

// skipString returns the index of the string terminator (ST, as ESC \ or
// 0x9c; BEL too for an OSC) of the control string whose payload starts at i,
// or the last index when it is not terminated.
func skipString(runes []rune, i int, bel bool) int {
	for ; i < len(runes); i++ {
		switch {
		case runes[i] == 0x9c, bel && runes[i] == '\a':
			return i
		case runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '\\':
			return i + 1
		}
	}
	return len(runes) - 1
}

// skipEscape returns the index of the final byte of the ESC sequence whose
// intermediates (0x20-0x2f) or final byte start at i.
func skipEscape(runes []rune, i int) int {
	for ; i < len(runes); i++ {
		if runes[i] >= 0x30 && runes[i] <= 0x7e {
			return i
		}
	}
	return len(runes) - 1
}
