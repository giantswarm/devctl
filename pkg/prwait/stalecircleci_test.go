package prwait

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func workflowListing(status string) sequence.Response {
	return body(map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": status}}})
}

func workflowJobs(statuses ...string) sequence.Response {
	jobs := []any{}
	for _, status := range statuses {
		jobs = append(jobs, map[string]any{"name": "job", "status": status})
	}
	return body(map[string]any{"items": jobs})
}

func Test_Wait_runningCircleCIWorkflow(t *testing.T) {
	const jobsPath = "GET /api/v2/workflow/w1/job"
	testCases := []struct {
		name              string
		workflows         []sequence.Response
		jobs              []sequence.Response
		more              sequence.Routes
		wantExit          int
		wantPolls         int
		wantJobReads      int
		wantJobsCompleted bool
	}{
		{
			name:              "case 0: stale running workflow with successful jobs is green at once",
			jobs:              []sequence.Response{workflowJobs("success", "success")},
			wantPolls:         1,
			wantJobReads:      1,
			wantJobsCompleted: true,
		},
		{
			name:         "case 1: a workflow finishing during the wait is green at the next poll",
			workflows:    []sequence.Response{workflowListing("running"), workflowListing("success")},
			jobs:         []sequence.Response{workflowJobs("success", "blocked")},
			wantPolls:    2,
			wantJobReads: 1,
		},
		{
			name: "case 2: a downstream job on a later page finishes while the workflow stays running",
			jobs: []sequence.Response{body(map[string]any{
				"items": []any{map[string]any{"name": "build", "status": "success"}}, "next_page_token": "next",
			})},
			more: sequence.Routes{
				jobsPath + "?page-token=next": {workflowJobs("blocked"), workflowJobs("success")},
			},
			wantPolls:         2,
			wantJobReads:      4,
			wantJobsCompleted: true,
		},
		{
			name:              "case 3: an invisible job list is read again next poll",
			jobs:              []sequence.Response{{Status: http.StatusNotFound}, workflowJobs("success")},
			wantPolls:         2,
			wantJobReads:      2,
			wantJobsCompleted: true,
		},
		{
			name:         "case 4: an empty job list cannot prove completion",
			jobs:         []sequence.Response{workflowJobs()},
			wantExit:     agentcli.ExitTimeout,
			wantPolls:    2,
			wantJobReads: 2,
		},
		{
			name:         "case 5: a failed job cannot prove completion despite green statuses",
			jobs:         []sequence.Response{workflowJobs("success", "failed")},
			wantExit:     agentcli.ExitTimeout,
			wantPolls:    2,
			wantJobReads: 2,
		},
		{
			name:         "case 6: an approval job still on hold keeps waiting",
			jobs:         []sequence.Response{workflowJobs("success", "on_hold")},
			wantExit:     agentcli.ExitTimeout,
			wantPolls:    2,
			wantJobReads: 2,
		},
		{
			name:         "case 7: a job whose state is unknown keeps waiting",
			jobs:         []sequence.Response{workflowJobs("success", "")},
			wantExit:     agentcli.ExitTimeout,
			wantPolls:    2,
			wantJobReads: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			gh[statPath] = []sequence.Response{successStatuses("ci/circleci: build")}
			cc := sequence.Routes{
				projPath: {project}, pipePath: {pipelines},
				wfPath: {workflowListing("running")}, jobsPath: tc.jobs,
			}
			if tc.workflows != nil {
				cc[wfPath] = tc.workflows
			}
			for path, responses := range tc.more {
				cc[path] = responses
			}
			h := start(t, gh, cc, true, 2*MinInterval)
			result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if got := exitCode(err); got != tc.wantExit {
				t.Fatalf("want exit %d, got %d: %v", tc.wantExit, got, err)
			}
			reads := map[string]int{}
			for _, request := range h.circleci.Requests() {
				key := request.String()
				if strings.HasSuffix(request.Path, "/job") {
					key = request.Method + " " + request.Path
				}
				reads[key]++
			}
			wantReads := map[string]int{projPath: 1, pipePath: tc.wantPolls, wfPath: tc.wantPolls, jobsPath: tc.wantJobReads}
			if diff := cmp.Diff(wantReads, reads); diff != "" {
				t.Errorf("CircleCI requests (-want +got):\n%s", diff)
			}
			if result.CircleCI == nil || len(result.CircleCI.Workflows) != 1 {
				t.Fatalf("want the workflow in the result, got %+v", result.CircleCI)
			}
			workflow := result.CircleCI.Workflows[0]
			if workflow.JobsCompleted != tc.wantJobsCompleted {
				t.Errorf("want jobsCompleted %v, got %+v", tc.wantJobsCompleted, workflow)
			}
			if tc.wantJobsCompleted && workflow.Status != "running" {
				t.Errorf("want the API's running status preserved, got %+v", workflow)
			}
			if tc.wantExit == agentcli.ExitTimeout {
				if !strings.Contains(err.Error(), "circleci workflow build (running)") {
					t.Errorf("timeout must name the unfinished workflow: %v", err)
				}
			} else if len(result.Unfinished) > 0 {
				t.Errorf("green head has unfinished items: %v", result.Unfinished)
			}
		})
	}
}

func Test_Wait_circleCIJobProofRequiresGreenStatuses(t *testing.T) {
	testCases := []struct {
		name     string
		statuses sequence.Response
		wantExit int
	}{
		{name: "case 0: no CircleCI status", statuses: noStatuses, wantExit: agentcli.ExitTimeout},
		{name: "case 1: another CI is green", statuses: successStatuses("other-ci"), wantExit: agentcli.ExitTimeout},
		{
			name: "case 2: a CircleCI job is pending",
			statuses: body(map[string]any{"statuses": []any{
				map[string]any{"context": "ci/circleci: build", "state": "success"},
				map[string]any{"context": "ci/circleci: test", "state": "pending"},
			}}),
			wantExit: agentcli.ExitTimeout,
		},
		{
			name: "case 3: a CircleCI job failed",
			statuses: body(map[string]any{"statuses": []any{
				map[string]any{"context": "ci/circleci: build", "state": "failure"},
			}}),
			wantExit: agentcli.ExitRed,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			gh[statPath] = []sequence.Response{tc.statuses}
			cc := sequence.Routes{projPath: {project}, pipePath: {pipelines}, wfPath: {workflowListing("running")}}
			h := start(t, gh, cc, true, 2*MinInterval)
			_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if got := exitCode(err); got != tc.wantExit {
				t.Fatalf("want exit %d, got %d: %v", tc.wantExit, got, err)
			}
			for _, request := range h.circleci.Requests() {
				if strings.HasSuffix(request.Path, "/job") {
					t.Error("jobs read without successful CircleCI statuses")
				}
			}
		})
	}
}
