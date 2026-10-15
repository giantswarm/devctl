package releasewait

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// logBlobs serves what a log redirect or a step's output URL points at:
// path to body, read without a token.
func logBlobs(t *testing.T, content map[string]string) *httptest.Server {
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

// failedTagPipeline is a generated repository whose tag pipeline's build
// workflow failed in job 99, whose failed step printed a test failure.
func failedTagPipeline(t *testing.T, blobURL string) fixture {
	circleci := pipelineRoutes(
		[][]map[string]any{{wf("w1", "build", "failed", "2026-09-21T10:00:00Z")}},
		map[string][]map[string]any{"w1": {
			{"name": "go-build", "status": "success", "type": "build", "job_number": 98},
			{"name": "push-to-registries-release", "status": "failed", "type": "build", "job_number": 99},
		}},
	)
	circleci["GET /api/v1.1/project/github/giantswarm/kserve/99"] = []sequence.Response{{Body: map[string]any{"steps": []any{
		map[string]any{"name": "checkout", "actions": []any{map[string]any{"failed": nil, "output_url": blobURL + "/circleci/checkout"}}},
		map[string]any{"name": "push", "actions": []any{map[string]any{"failed": true, "output_url": blobURL + "/circleci/push"}}},
	}}}}
	return fixture{
		entry:    generatedEntry(t),
		github:   baseGitHub([]string{"Dockerfile", "helm"}, []string{"zz_generated.auto_release.yaml"}, []string{"config.yml", "workflows.yml"}),
		circleci: circleci,
	}
}

// --failed-log on a failed tag pipeline reads each failed job's failed
// steps once and keeps the tail of their output.
func TestWaitFailedLogReadsTheFailedJobsOfTheTagPipeline(t *testing.T) {
	blob := logBlobs(t, map[string]string{
		"/circleci/push": `[{"message":"pushing gsoci.azurecr.io/giantswarm/kserve-controller:1.2.3\r\n"},{"message":"\u001b[31m503 Service Unavailable\u001b[0m\r\nexit status 1\r\n"}]`,
	})
	fx := failedTagPipeline(t, blob.URL)
	fx.failedLogLines = 2

	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitRed, "build/push-to-registries-release")
	want := []prwait.FailedJob{{
		Name:    "build/push-to-registries-release",
		Source:  prwait.SourceCircleCI,
		URL:     "https://app.circleci.com/pipelines/github/giantswarm/kserve/12/workflows/w1/jobs/99",
		LogTail: "503 Service Unavailable\nexit status 1",
	}}
	if diff := cmp.Diff(want, result.FailedJobs); diff != "" {
		t.Errorf("failedJobs:\n%s", diff)
	}
}

// Without --failed-log the document carries no failedJobs and no log is read.
func TestWaitWithoutFailedLogLeavesTheDocumentUnchanged(t *testing.T) {
	blob := logBlobs(t, map[string]string{})
	result, err := run(t, failedTagPipeline(t, blob.URL))
	assertExit(t, err, agentcli.ExitRed, "build/push-to-registries-release")
	if result.FailedJobs != nil {
		t.Errorf("failedJobs without --failed-log: %+v", result.FailedJobs)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["failedJobs"]; ok {
		t.Errorf("the document carries failedJobs without --failed-log: %s", raw)
	}
}

// --failed-log on a tag judged by its Actions runs reads the failed jobs of
// each failed run, up to the failed step's error.
func TestWaitFailedLogReadsTheFailedJobsOfTheTagActionsRuns(t *testing.T) {
	blob := logBlobs(t, map[string]string{
		"/actions/7": "2026-10-01T10:00:00Z building\n2026-10-01T10:00:01Z upload failed: 502\n2026-10-01T10:00:02Z ##[error]Process completed with exit code 1.\n2026-10-01T10:00:03Z Post job cleanup.\n",
	})
	github := baseGitHub([]string{"main.go"}, []string{"zz_generated.auto_release.yaml"}, nil)
	github[repoRoute("/actions/runs")] = []sequence.Response{{Body: map[string]any{"total_count": 1, "workflow_runs": []map[string]any{
		{"id": 2, "name": "Release binaries", "event": "push", "head_branch": testTag, "status": "completed", "conclusion": "failure", "html_url": "https://example/runs/2"},
	}}}}
	github[repoRoute("/actions/runs/2/jobs")] = []sequence.Response{{Body: map[string]any{"total_count": 2, "jobs": []map[string]any{
		{"id": 6, "name": "lint", "status": "completed", "conclusion": "success", "html_url": "https://example/runs/2/job/6"},
		{"id": 7, "name": "upload", "status": "completed", "conclusion": "failure", "html_url": "https://example/runs/2/job/7"},
	}}}}
	github[repoRoute("/actions/jobs/7/logs")] = []sequence.Response{{Status: http.StatusFound, Headers: map[string]string{"Location": blob.URL + "/actions/7"}}}

	result, err := run(t, fixture{github: github, failedLogLines: 2})
	assertExit(t, err, agentcli.ExitRed, "Release binaries")
	want := []prwait.FailedJob{{
		Name:    "Release binaries/upload",
		Source:  prwait.SourceActions,
		URL:     "https://example/runs/2/job/7",
		LogTail: "2026-10-01T10:00:01Z upload failed: 502\n2026-10-01T10:00:02Z ##[error]Process completed with exit code 1.",
	}}
	if diff := cmp.Diff(want, result.FailedJobs); diff != "" {
		t.Errorf("failedJobs:\n%s", diff)
	}
}

// A log that cannot be read is named in its job; the verdict stands.
func TestWaitFailedLogUnreadableKeepsTheVerdict(t *testing.T) {
	blob := logBlobs(t, map[string]string{})
	fx := failedTagPipeline(t, blob.URL)
	fx.failedLogLines = 50

	result, err := run(t, fx)
	assertExit(t, err, agentcli.ExitRed, "build/push-to-registries-release")
	if len(result.FailedJobs) != 1 || result.FailedJobs[0].LogError == "" || result.FailedJobs[0].LogTail != "" {
		t.Errorf("failedJobs: %+v", result.FailedJobs)
	}
}
