package ci

import (
	"context"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// routes7 is job 7 of o/r, shaped after CircleCI's answers: it failed in its
// "Build image" step, after a passed checkout.
func routes7(outputURL string) sequence.Routes {
	return sequence.Routes{
		"GET /api/v2/project/gh/o/r/job/7": {{Body: map[string]any{
			"number": 7, "name": "push-go", "status": "failed", "web_url": "https://circleci.com/gh/o/r/7",
			"started_at": "2026-10-22T23:25:44.983Z", "stopped_at": "2026-10-22T23:25:47.057Z",
			"pipeline": map[string]any{"id": "p1"}, "latest_workflow": map[string]any{"id": "w1"},
		}}},
		"GET /api/v1.1/project/github/o/r/7": {{Body: map[string]any{"build_num": 7, "steps": []any{
			map[string]any{"name": "Checkout code", "actions": []any{map[string]any{"name": "Checkout code", "index": 0, "status": "success", "failed": nil,
				"start_time": "2026-10-22T23:25:45.178Z", "end_time": "2026-10-22T23:25:45.744Z", "output_url": outputURL + "/checkout"}}},
			map[string]any{"name": "Build image", "actions": []any{map[string]any{"name": "Build image", "index": 0, "status": "failed", "failed": true,
				"start_time": "2026-10-22T23:25:46.928Z", "end_time": "2026-10-22T23:25:46.954Z", "output_url": outputURL + "/build"}}},
		}}}},
	}
}

const buildOutput = `[{"message": "Building for platforms: linux/amd64\r\n", "time": "2026-10-22T23:25:46.930Z", "type": "out"},
	{"message": "\u001b[31mNo valid registry/tag combinations found for push. Exiting.\u001b[0m\r\n", "time": "2026-10-22T23:25:46.950Z", "type": "out"},
	{"message": "\r\nExited with code exit status 1\r\n", "time": "2026-10-22T23:25:46.954Z", "type": "err"}]`

func readJob(t *testing.T, client *circleciclient.Client, ref string, lines int) (*JobResult, []string, error) {
	t.Helper()
	result := NewJobResult("o/r")
	var warnings []string
	err := ReadJob(context.Background(), client, "o", "r", ref, lines, result, func(w string) { warnings = append(warnings, w) })
	return result, warnings, err
}

// TestReadJobReadsStepsAndFailedOutput: the job by its number, every step
// with its status, and the failed step's output without control sequences;
// a passed step's output is not read.
func TestReadJobReadsStepsAndFailedOutput(t *testing.T) {
	output := stepOutput(t, map[string]string{"/build": buildOutput})
	client, _ := newClient(t, routes7(output.URL))
	result, warnings, err := readJob(t, client, "7", DefaultJobLogLines)
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %v", warnings)
	}
	j := result.Job
	if j == nil || j.Number != 7 || j.Name != "push-go" || j.Status != "failed" || j.URL != "https://circleci.com/gh/o/r/7" || j.PipelineID != "p1" || j.WorkflowID != "w1" || j.StartedAt == nil || j.StoppedAt == nil {
		t.Fatalf("job: %+v", j)
	}
	if len(j.Steps) != 2 || j.Steps[0].Status != "success" || j.Steps[1].Name != "Build image" || j.Steps[1].Status != "failed" || j.Steps[1].EndedAt == nil {
		t.Errorf("steps: %+v", j.Steps)
	}
	if j.LogError != "" {
		t.Errorf("log error: %s", j.LogError)
	}
	if !strings.Contains(j.LogTail, "No valid registry/tag combinations found for push. Exiting.") || strings.Contains(j.LogTail, "\u001b") || strings.Contains(j.LogTail, "\r") {
		t.Errorf("log tail: %q", j.LogTail)
	}
}

// TestReadJobKeepsTheLastLines: --lines bounds the tail.
func TestReadJobKeepsTheLastLines(t *testing.T) {
	output := stepOutput(t, map[string]string{"/build": buildOutput})
	client, _ := newClient(t, routes7(output.URL))
	result, _, err := readJob(t, client, "7", 1)
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	if got := strings.TrimSpace(result.Job.LogTail); got != "Exited with code exit status 1" {
		t.Errorf("one line: %q", got)
	}
}

// TestReadJobOutputUnreadable: output CircleCI does not answer is the job's
// logError; the steps are still read.
func TestReadJobOutputUnreadable(t *testing.T) {
	output := stepOutput(t, map[string]string{})
	client, _ := newClient(t, routes7(output.URL))
	result, _, err := readJob(t, client, "7", DefaultJobLogLines)
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	if result.Job.LogError == "" || result.Job.LogTail != "" || len(result.Job.Steps) != 2 {
		t.Errorf("job: %+v", result.Job)
	}
}

// TestReadJobUnknown: a job CircleCI does not know is exit 3; a number that
// is none is a usage error.
func TestReadJobUnknown(t *testing.T) {
	client, _ := newClient(t, sequence.Routes{})
	if _, _, err := readJob(t, client, "8", DefaultJobLogLines); agentcli.Exit(err) != agentcli.ExitNotApplicable {
		t.Errorf("unknown job: exit %d (%v)", agentcli.Exit(err), err)
	}
	if _, _, err := readJob(t, client, "seven", DefaultJobLogLines); agentcli.Exit(err) != agentcli.ExitUsage {
		t.Errorf("not a number: exit %d (%v)", agentcli.Exit(err), err)
	}
}
