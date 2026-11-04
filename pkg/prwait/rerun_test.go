package prwait

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

// A rerun is already visible in CircleCI before its jobs replace their
// failed GitHub statuses. Exercise the actual waiter, including required
// contexts, cached GitHub responses and a delayed first pending status.
func Test_Wait_CircleCIRerun(t *testing.T) {
	const contextName = "ci/circleci: test"
	status := func(state, at string) sequence.Response {
		return body(map[string]any{"statuses": []any{
			map[string]any{"context": contextName, "state": state, "updated_at": at, "target_url": "https://circleci.com/gh/o/r/10"},
			map[string]any{"context": "ci/circleci: compile", "state": "success", "updated_at": "2026-10-10T10:01:00Z"},
		}})
	}
	workflows := func(state string) sequence.Response {
		return body(map[string]any{"items": []any{
			map[string]any{"id": "old", "name": "build", "status": "failed", "created_at": "2026-10-10T10:00:00Z"},
			map[string]any{"id": "new", "name": "build", "status": state, "created_at": "2026-10-10T10:10:00Z"},
		}})
	}
	for _, tc := range []struct {
		name     string
		oldState string
		state    string
		workflow string
		optional bool
		wantExit int
	}{
		{name: "case 0: rerun passes", oldState: "failure", state: "success", workflow: "success"},
		{name: "case 1: rerun fails", oldState: "failure", state: "failure", workflow: "failed", wantExit: agentcli.ExitRed},
		{name: "case 2: rerun never posts", oldState: "failure", wantExit: agentcli.ExitTimeout},
		{name: "case 3: nonrequired rerun passes", oldState: "failure", state: "success", workflow: "success", optional: true},
		{name: "case 4: errored job rerun passes", oldState: "error", state: "success", workflow: "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			if !tc.optional {
				gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{body(map[string]any{
					"required_status_checks": map[string]any{"contexts": []string{contextName}},
				})}
			}
			old := status(tc.oldState, "2026-10-10T10:05:00Z")
			gh[statPath] = []sequence.Response{old, old}
			cc := sequence.Routes{
				projPath: {project}, listPath: {pipelines}, pipePath: {pipelines},
				wfPath: {workflows("running"), workflows("running")},
				"GET /api/v2/workflow/old/job": {body(map[string]any{"items": []any{
					map[string]any{"name": "compile", "status": "success"},
					map[string]any{"name": "test", "status": "failed"},
				}})},
			}
			if tc.state != "" {
				gh[statPath] = append(gh[statPath], status("pending", "2026-10-10T10:11:00Z"), status(tc.state, "2026-10-10T10:12:00Z"))
				cc[wfPath] = append(cc[wfPath], workflows("running"), workflows(tc.workflow))
			}
			h := start(t, gh, cc, true, 2*time.Minute)
			result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if got := exitCode(err); got != tc.wantExit {
				t.Fatalf("want exit %d, got %d (%v)", tc.wantExit, got, err)
			}
			if !strings.Contains(h.progress.String(), "status "+contextName+" (awaiting rerun status)") {
				t.Errorf("want the stale status to wait for the rerun:\n%s", h.progress.String())
			}
			polls := 0
			for _, request := range h.circleci.Requests() {
				if request.Path == "/api/v2/pipeline/p1/workflow" {
					polls++
				}
			}
			if polls < 4 {
				t.Errorf("want at least four polls through the rerun, got %d", polls)
			}
			if h.cond.Replayed() == 0 {
				t.Error("want cached GitHub responses while the failed status stays unchanged")
			}
			if tc.state == "" {
				for _, check := range result.Checks {
					if check.Name == contextName && (check.Status != statusPending || check.Conclusion != "") {
						t.Errorf("stale failure must be pending in the document: %+v", check)
					}
				}
			} else if result.CircleCI == nil || len(result.CircleCI.Workflows) != 1 || result.CircleCI.Workflows[0].Status != tc.workflow {
				t.Errorf("want only the rerun's final workflow, got %+v", result.CircleCI)
			}
			for _, check := range result.Checks {
				if check.Name == "ci/circleci: compile" && (check.Status != statusCompleted || check.Conclusion != "success") {
					t.Errorf("want the successful job reused by rerun-from-failed: %+v", check)
				}
			}
		})
	}
}

func Test_Wait_CircleCIRerunGuards(t *testing.T) {
	old := circleciclient.Workflow{ID: "old", Name: "build", Status: "failed", CreatedAt: time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)}
	newest := circleciclient.Workflow{ID: "new", Name: "build", Status: "running", CreatedAt: old.CreatedAt.Add(10 * time.Minute)}
	for _, tc := range []struct {
		name      string
		context   string
		at        time.Time
		workflows []circleciclient.Workflow
		jobs      []circleciclient.Job
		wantReads int
		wantExit  int
		jobHTTP   int
	}{
		{name: "case 0: no rerun stays red immediately", context: "ci/circleci: test", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{old}, wantExit: agentcli.ExitRed},
		{name: "case 1: another workflow running is not a rerun", context: "ci/circleci: test", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{old, {ID: "other", Name: "release", Status: "running", CreatedAt: newest.CreatedAt}}, wantExit: agentcli.ExitRed},
		{name: "case 2: new failure is red", context: "ci/circleci: test", at: newest.CreatedAt.Add(time.Second), workflows: []circleciclient.Workflow{old, newest}, wantExit: agentcli.ExitRed},
		{name: "case 3: timestamp equal to rerun is not superseded", context: "ci/circleci: test", at: newest.CreatedAt, workflows: []circleciclient.Workflow{old, newest}, wantExit: agentcli.ExitRed},
		{name: "case 4: missing status time is not superseded", context: "ci/circleci: test", workflows: []circleciclient.Workflow{old, newest}, wantExit: agentcli.ExitRed},
		{name: "case 5: missing rerun time is not superseded", context: "ci/circleci: test", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{{ID: "old", Name: "build", Status: "failed"}, {ID: "new", Name: "build", Status: "running"}}, wantExit: agentcli.ExitRed},
		{name: "case 6: another CI stays red", context: "other-ci", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{old, newest}, wantExit: agentcli.ExitRed},
		{name: "case 7: another job stays red", context: "ci/circleci: security", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{old, newest}, jobs: []circleciclient.Job{{Name: "test"}}, wantReads: 1, wantExit: agentcli.ExitRed},
		{name: "case 8: empty old job listing cannot establish ownership", context: "ci/circleci: test", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{old, newest}, wantReads: 1, wantExit: agentcli.ExitRed},
		{name: "case 9: refused job read is not a verdict", context: "ci/circleci: test", at: old.CreatedAt.Add(time.Minute), workflows: []circleciclient.Workflow{old, newest}, jobHTTP: 403, wantReads: 1, wantExit: agentcli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := gitHubGreen()
			gh[confPath] = []sequence.Response{config}
			gh[statPath] = []sequence.Response{body(map[string]any{"statuses": []any{map[string]any{
				"context": tc.context, "state": "failure", "updated_at": tc.at,
			}}})}
			cc := sequence.Routes{
				projPath: {project}, listPath: {pipelines}, pipePath: {pipelines},
				wfPath:                         {body(map[string]any{"items": tc.workflows})},
				"GET /api/v2/workflow/old/job": {{Status: tc.jobHTTP, Body: map[string]any{"items": tc.jobs}}},
			}
			h := start(t, gh, cc, true, time.Minute)
			_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if got := exitCode(err); got != tc.wantExit {
				t.Fatalf("want exit %d, got %d (%v)", tc.wantExit, got, err)
			}
			polls, reads := 0, 0
			for _, request := range h.circleci.Requests() {
				switch request.Path {
				case "/api/v2/pipeline/p1/workflow":
					polls++
				case "/api/v2/workflow/old/job":
					reads++
				}
			}
			if polls != 1 || reads != tc.wantReads {
				t.Errorf("want one poll and %d old job reads, got %d polls and %d reads", tc.wantReads, polls, reads)
			}
		})
	}
}
