package prwait

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// blobs serves what a log redirect points at: path to body.
func blobs(t *testing.T, content map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Circle-Token") != "" {
			t.Errorf("%s: the signed log URL is read without the token", r.URL.Path)
		}
		body, ok := content[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func actionsRun(id int, name, conclusion string) map[string]any {
	r := run(name, "completed", conclusion)
	r["id"] = id
	r["html_url"] = fmt.Sprintf("https://github.com/o/r/actions/runs/1/job/%d", id)
	r["app"] = map[string]any{"slug": "github-actions"}
	return r
}

func Test_Wait_failedLogReadsEachFailedJobOnce(t *testing.T) {
	blob := blobs(t, map[string]string{
		"/actions/7":     "2026-10-01T10:00:00Z one\n2026-10-01T10:00:01Z \x1b[31mtwo\x1b[0m\r\n2026-10-01T10:00:02Z ##[error]Process completed with exit code 1.\n2026-10-01T10:00:03Z Post job cleanup.\n",
		"/circleci/step": `[{"message":"go test ./...\r\n"},{"message":"--- FAIL: Test_x\r\nFAIL\r\n"}]`,
	})
	gh := gitHubGreen()
	gh[confPath] = []sequence.Response{config}
	gh[runsPath] = []sequence.Response{checkRuns(
		actionsRun(7, "lint", "failure"),
		actionsRun(8, "build", "success"),
		run("ci/circleci: go-test", "completed", "failure"),
	)}
	gh["GET /repos/o/r/actions/jobs/7/logs"] = []sequence.Response{{Status: http.StatusFound, Headers: map[string]string{"Location": blob.URL + "/actions/7"}}}
	cc := sequence.Routes{
		projPath: {project},
		listPath: {pipelines},
		pipePath: {pipelines},
		wfPath:   {body(map[string]any{"items": []any{map[string]any{"id": "w1", "name": "go-build", "status": "failed"}}})},
		"GET /api/v2/workflow/w1/job": {body(map[string]any{"items": []any{
			map[string]any{"name": "go-test", "status": "failed", "type": "build", "job_number": 99},
			map[string]any{"name": "go-lint", "status": "success", "type": "build", "job_number": 98},
		}})},
		"GET /api/v1.1/project/github/o/r/99": {body(map[string]any{"steps": []any{
			map[string]any{"name": "checkout", "actions": []any{map[string]any{"failed": nil, "output_url": blob.URL + "/circleci/checkout"}}},
			map[string]any{"name": "test", "actions": []any{map[string]any{"failed": true, "output_url": blob.URL + "/circleci/step"}}},
		}})},
	}
	h := start(t, gh, cc, true, 2*time.Minute)
	h.waiter.failedLogLines = 2

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRed {
		t.Fatalf("want exit 1, got %d %v", exitCode(err), err)
	}
	want := []FailedJob{
		{Name: "lint", Source: SourceActions, URL: "https://github.com/o/r/actions/runs/1/job/7", LogTail: "2026-10-01T10:00:01Z two\n2026-10-01T10:00:02Z ##[error]Process completed with exit code 1."},
		{Name: "go-build/go-test", Source: SourceCircleCI, URL: "https://app.circleci.com/pipelines/github/o/r/12/workflows/w1/jobs/99", LogTail: "--- FAIL: Test_x\nFAIL"},
	}
	if diff := cmp.Diff(want, result.FailedJobs); diff != "" {
		t.Errorf("failedJobs:\n%s", diff)
	}

	var printed strings.Builder
	PrintFailedJobs(&printed, result.FailedJobs)
	if !strings.Contains(printed.String(), "failed job lint (https://github.com/o/r/actions/runs/1/job/7):\n2026-10-01T10:00:01Z two\n") {
		t.Errorf("stderr:\n%s", printed.String())
	}
}

func Test_Wait_failedLogUnreadableKeepsTheVerdict(t *testing.T) {
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns(actionsRun(7, "lint", "failure"))}
	gh["GET /repos/o/r/actions/jobs/7/logs"] = []sequence.Response{{Status: http.StatusGone, Body: map[string]any{"message": "logs expired"}}}
	h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
	h.waiter.failedLogLines = 50

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRed {
		t.Fatalf("want exit 1, got %d %v", exitCode(err), err)
	}
	if len(result.FailedJobs) != 1 || result.FailedJobs[0].LogError == "" || result.FailedJobs[0].LogTail != "" {
		t.Errorf("want the job named with its log error, got %+v", result.FailedJobs)
	}
}

func Test_Wait_failedLogReadsNothingUnlessRed(t *testing.T) {
	for name, enabled := range map[string]int{"green with --failed-log": 50, "red without": 0} {
		t.Run(name, func(t *testing.T) {
			gh := gitHubGreen()
			if enabled == 0 {
				gh[runsPath] = []sequence.Response{checkRuns(actionsRun(7, "lint", "failure"))}
			}
			h := start(t, gh, sequence.Routes{}, false, 2*time.Minute)
			h.waiter.failedLogLines = enabled
			result, _ := h.waiter.Wait(context.Background(), "o", "r", 42)
			if result.FailedJobs != nil {
				t.Errorf("want no failedJobs, got %+v", result.FailedJobs)
			}
			for _, req := range h.github.Requests() {
				if strings.Contains(req.Path, "/logs") {
					t.Errorf("want no log request, got %s", req)
				}
			}
		})
	}
}

func Test_tailLines(t *testing.T) {
	tests := map[string]struct {
		in    string
		n     int
		until string
		want  string
	}{
		"fewer lines than n":     {in: "a\nb\n", n: 5, want: "a\nb"},
		"exactly n":              {in: "a\nb\nc", n: 3, want: "a\nb\nc"},
		"more lines than n":      {in: "a\nb\nc\nd\ne\n", n: 2, want: "d\ne"},
		"empty":                  {in: "", n: 3, want: ""},
		"escapes and CRs gone":   {in: "\x1b[1;32mok\x1b[0m\r\n", n: 1, want: "ok"},
		"ends at the last until": {in: "a\n##[error]x\nb\n##[error]exit 1\ncleanup\n", n: 2, until: "##[error]", want: "b\n##[error]exit 1"},
		"until absent":           {in: "a\nb\nc\n", n: 2, until: "##[error]", want: "b\nc"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tailLines(strings.NewReader(tc.in), tc.n, tc.until)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func Test_FailedLog_TailLines(t *testing.T) {
	if n, err := (FailedLog{Lines: 50}).TailLines(); n != 0 || err != nil {
		t.Errorf("without --failed-log: got %d %v, want 0", n, err)
	}
	if n, err := (FailedLog{Enabled: true, Lines: 20}).TailLines(); n != 20 || err != nil {
		t.Errorf("got %d %v, want 20", n, err)
	}
	if _, err := (FailedLog{Enabled: true}).TailLines(); err == nil {
		t.Error("want an error for --failed-log-lines 0")
	}
}
