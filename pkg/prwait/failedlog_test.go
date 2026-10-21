package prwait

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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

func Test_Wait_failedLogNamesTheRedRunOfAMatrix(t *testing.T) {
	blob := blobs(t, map[string]string{"/actions/4": "plan failed\n"})
	gh := gitHubGreen()
	var jobs []map[string]any
	for id := 1; id <= 10; id++ {
		conclusion := "success"
		if id == 4 {
			conclusion = "failure"
		}
		job := actionsRun(id, "plan / tofu-plan", conclusion)
		job["check_suite"] = map[string]any{"id": 100}
		jobs = append(jobs, job)
	}
	gh[runsPath] = []sequence.Response{checkRuns(jobs...)}
	gh[actsPath] = []sequence.Response{body(map[string]any{"total_count": 1, "workflow_runs": []any{
		map[string]any{"id": 1, "name": "plan", "workflow_id": 5, "check_suite_id": 100, "status": "completed", "conclusion": "failure", "html_url": "https://github.com/o/r/actions/runs/1"},
	}})}
	gh["GET /repos/o/r/actions/jobs/4/logs"] = []sequence.Response{{Status: http.StatusFound, Headers: map[string]string{"Location": blob.URL + "/actions/4"}}}
	h := start(t, gh, nil, false, 2*time.Minute)
	h.waiter.failedLogLines = 5

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if exitCode(err) != agentcli.ExitRed {
		t.Fatalf("want exit 1, got %d %v", exitCode(err), err)
	}
	if !strings.Contains(err.Error(), "check plan / tofu-plan [plan, check run 4] concluded failure") {
		t.Errorf("verdict does not name the red run: %v", err)
	}
	want := []FailedJob{{Name: "plan / tofu-plan [plan, check run 4]", Source: SourceActions, URL: "https://github.com/o/r/actions/runs/1/job/4", LogTail: "plan failed"}}
	if diff := cmp.Diff(want, result.FailedJobs); diff != "" {
		t.Errorf("failedJobs:\n%s", diff)
	}
	if len(result.Checks) != 10 {
		t.Fatalf("want every run of the name listed, got %d checks", len(result.Checks))
	}
	for i, check := range result.Checks {
		if check.ID != int64(i+1) || check.Workflow != "plan" {
			t.Errorf("checks[%d]: want id %d of workflow plan, got %+v", i, i+1, check)
		}
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
		"OSC 52 gone whole":      {in: "a\x1b]52;c;ZWNobyBwd25lZA==\x07b\n", n: 1, want: "ab"},
		"OSC 8 keeps its text":   {in: "\x1b]8;;https://evil.example\x1b\\docs\x1b]8;;\x1b\\\n", n: 1, want: "docs"},
		"DCS gone whole":         {in: "a\x1bP1$r0m\x1b\\b\n", n: 1, want: "ab"},
		"unterminated OSC":       {in: "a\x1b]0;title\nb\n", n: 2, want: "a\nb"},
		"bare ESC":               {in: "a\x1bb\x1b\n", n: 1, want: "a"},
		"charset designation":    {in: "a\x1b(Bb\n", n: 1, want: "ab"},
		"C0 controls":            {in: "a\x07\x08\x00b\rc\x7f\n", n: 1, want: "abc"},
		"C1 controls":            {in: "a\u009b31mb\u009d0;t\u009cc\u0090q\u009cd\u0085e\n", n: 1, want: "abcde"},
		"bidi controls":          {in: "a\u202eb\u2066c\u2069\u200fd\n", n: 1, want: "abcd"},
		"tab and UTF-8 survive":  {in: "ok\tgrüße ✓ 日本 🐝 👩\u200d💻\n", n: 1, want: "ok\tgrüße ✓ 日本 🐝 👩\u200d💻"},
		"ends at the last until": {in: "a\n##[error]x\nb\n##[error]exit 1\ncleanup\n", n: 2, until: "##[error]", want: "##[error]x\nb\n##[error]exit 1"},
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

// logLines is lines, each ended by a newline.
func logLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// repeated is n lines of format, each formatted with its 1-based number.
func repeated(n int, format string) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf(format, i+1)
	}
	return lines
}

func Test_tailLines_firstFailure(t *testing.T) {
	// A CircleCI step running concurrently --group: Prettier fails in the
	// lint group, the test group's green output comes after it.
	circleci := logLines(slices.Concat(
		[]string{
			"$ yarn run ci:patches && concurrently --group --names test,tsc,lint ...",
			"[lint] Checking formatting...",
			"[lint] [warn] plugins/agents/src/Page.tsx",
			"[lint] [warn] Code style issues found in the above file. Run Prettier with --write to fix.",
			"[lint] yarn run ci:lint exited with code 1",
		},
		repeated(30, "[test] PASS plugins/p%d/src/index.test.ts"),
		[]string{"[test] yarn run test:all exited with code 0"},
	)...)
	// An Actions Go test step: the failing package's --- FAIL block comes
	// before the green packages after it and the run's summary.
	actions := logLines(slices.Concat(
		[]string{
			"2026-10-10T12:00:00.0000000Z ##[group]Run make test",
			"2026-10-10T12:00:01.0000000Z ok  \tgithub.com/o/r/pkg/a\t0.1s",
			"2026-10-10T12:00:02.0000000Z --- FAIL: TestReconcile (0.00s)",
			"2026-10-10T12:00:02.0000000Z     reconcile_test.go:42: got 1, want 2",
			"2026-10-10T12:00:02.0000000Z FAIL",
			"2026-10-10T12:00:02.0000000Z FAIL\tgithub.com/o/r/pkg/b\t0.2s",
		},
		repeated(30, "2026-10-10T12:00:03.0000000Z ok  \tgithub.com/o/r/pkg/c%d\t0.1s"),
		[]string{
			"2026-10-10T12:00:04.0000000Z make: *** [Makefile:12: test] Error 1",
			"2026-10-10T12:00:04.0000000Z ##[error]Process completed with exit code 2.",
			"2026-10-10T12:00:05.0000000Z Post job cleanup.",
		},
	)...)

	tests := map[string]struct {
		in    string
		n     int
		until string
		want  []string
	}{
		"CircleCI: the failing group leads the green one": {
			in: circleci, n: 5,
			want: []string{
				"[lint] [warn] plugins/agents/src/Page.tsx",
				"[lint] [warn] Code style issues found in the above file. Run Prettier with --write to fix.",
				"[lint] yarn run ci:lint exited with code 1",
				"[test] PASS plugins/p1/src/index.test.ts",
				"[test] PASS plugins/p2/src/index.test.ts",
				"[... 24 lines ...]",
				"[test] PASS plugins/p27/src/index.test.ts",
				"[test] PASS plugins/p28/src/index.test.ts",
				"[test] PASS plugins/p29/src/index.test.ts",
				"[test] PASS plugins/p30/src/index.test.ts",
				"[test] yarn run test:all exited with code 0",
			},
		},
		"Actions: the failing Go package leads the summary": {
			in: actions, n: 5, until: actionsError,
			want: []string{
				"2026-10-10T12:00:01.0000000Z ok  \tgithub.com/o/r/pkg/a\t0.1s",
				"2026-10-10T12:00:02.0000000Z --- FAIL: TestReconcile (0.00s)",
				"2026-10-10T12:00:02.0000000Z     reconcile_test.go:42: got 1, want 2",
				"2026-10-10T12:00:02.0000000Z FAIL",
				"2026-10-10T12:00:02.0000000Z FAIL\tgithub.com/o/r/pkg/b\t0.2s",
				"[... 27 lines ...]",
				"2026-10-10T12:00:03.0000000Z ok  \tgithub.com/o/r/pkg/c28\t0.1s",
				"2026-10-10T12:00:03.0000000Z ok  \tgithub.com/o/r/pkg/c29\t0.1s",
				"2026-10-10T12:00:03.0000000Z ok  \tgithub.com/o/r/pkg/c30\t0.1s",
				"2026-10-10T12:00:04.0000000Z make: *** [Makefile:12: test] Error 1",
				"2026-10-10T12:00:04.0000000Z ##[error]Process completed with exit code 2.",
			},
		},
		"excerpt running into the tail joins it": {
			in: logLines("a", "b", "FAIL x", "c", "d", "e", "f"), n: 3,
			want: []string{"FAIL x", "c", "d", "e", "f"},
		},
		"a single failing last step as before": {
			in:    logLines(slices.Concat(repeated(10, "build %d"), []string{"--- FAIL: TestX (0.00s)", "FAIL", "##[error]Process completed with exit code 1.", "Post job cleanup."})...),
			n:     5,
			until: actionsError,
			want:  []string{"build 9", "build 10", "--- FAIL: TestX (0.00s)", "FAIL", "##[error]Process completed with exit code 1."},
		},
		"exit code 0 is no failure": {
			in: logLines("x exited with code 0", "a", "b", "c"), n: 2,
			want: []string{"b", "c"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := tailLines(strings.NewReader(tc.in), tc.n, tc.until)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(strings.Join(tc.want, "\n"), got); diff != "" {
				t.Errorf("tail (-want +got):\n%s", diff)
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

func Test_FailedJob_controls(t *testing.T) {
	job := FailedJob{
		Name:     "build\x1b]52;c;eA==\x07",
		Source:   SourceActions,
		URL:      "https://example.com/\x1b[2Jjob",
		LogError: "boom\u009b31m\r\n",
	}
	got, err := json.Marshal([]FailedJob{job})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"build","source":"actions","url":"https://example.com/job","logError":"boom"}]`
	if string(got) != want {
		t.Errorf("JSON: got %s, want %s", got, want)
	}
	var out strings.Builder
	PrintFailedJobs(&out, []FailedJob{job})
	if want := "failed job build (https://example.com/job): log unreadable: boom\n"; out.String() != want {
		t.Errorf("stderr: got %q, want %q", out.String(), want)
	}
}
