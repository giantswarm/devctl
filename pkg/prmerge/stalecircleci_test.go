package prmerge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

func Test_Merge_circleCIWorkflowCompletion(t *testing.T) {
	testCases := []struct {
		name      string
		second    string
		jobs      []sequence.Response
		wantProof bool
		wantExit  int
	}{
		{
			name: "case 0: a workflow finishing during the wait merges at the next poll", second: "success",
			jobs: []sequence.Response{{Body: map[string]any{"items": []any{map[string]any{"name": "test", "status": "blocked"}}}}},
		},
		{
			name: "case 1: successful jobs clear a stale running workflow at the next poll", second: "running", wantProof: true,
			jobs: []sequence.Response{
				{Body: map[string]any{"items": []any{map[string]any{"name": "test", "status": "blocked"}}}},
				{Body: map[string]any{"items": []any{map[string]any{"name": "test", "status": "success"}}}},
			},
		},
		{
			name: "case 2: a downstream job still blocked prevents a merge", second: "running", wantExit: agentcli.ExitTimeout,
			jobs: []sequence.Response{{Body: map[string]any{"items": []any{map[string]any{"name": "test", "status": "blocked"}}}}},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			listing := func(status string) sequence.Response {
				return sequence.Response{Body: map[string]any{"items": []any{map[string]any{"id": "w1", "name": "build", "status": status}}}}
			}
			cc, err := circlemock.Start(sequence.Routes{
				"GET /api/v2/project/gh/o/r": {{Body: map[string]any{"slug": "gh/o/r"}}},
				"GET /api/v2/project/gh/o/r/pipeline?branch=feature": {{Body: map[string]any{"items": []any{
					map[string]any{"id": "p1", "number": 12, "state": "created", "vcs": map[string]any{"revision": "abc123"}},
				}}}},
				"GET /api/v2/pipeline/p1/workflow": {listing("running"), listing(tc.second)},
				"GET /api/v2/workflow/w1/job":      tc.jobs,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cc.Close)
			clock := agentcli.NewVirtualClock(time.Now())
			started := clock.Now()
			h := newHarness(t, routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/contents/.circleci/config.yml": {{Body: map[string]any{"type": "file"}}},
				"GET /repos/o/r/commits/abc123/status": {{Body: map[string]any{"statuses": []any{
					map[string]any{"context": "ci/circleci: build", "state": "success"},
				}}}},
			}), func(config *Config) {
				config.Wait.Clock = clock
				config.Wait.Timeout = 2 * prwait.MinInterval
				config.Wait.CircleCI = func(context.Context) (*circleciclient.Client, error) {
					return circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: cc.URL})
				}
			})
			result, err := h.merge(t)
			if got := exitCode(err); got != tc.wantExit {
				t.Fatalf("want exit %d, got %d: %v", tc.wantExit, got, err)
			}
			wantMerges := 1
			if tc.wantExit != 0 {
				wantMerges = 0
			}
			if got := h.requested("PUT /repos/o/r/pulls/42/merge"); got != wantMerges {
				t.Errorf("want %d merge requests, got %d", wantMerges, got)
			}
			if tc.wantExit != 0 {
				if h.requested("DELETE /repos/o/r/git/refs/heads/feature") != 0 {
					t.Error("deleted the branch before the head was green")
				}
				return
			}
			if result.MergeCommitSHA != "m1" || len(result.Unfinished) != 0 {
				t.Errorf("want a merged result without unfinished items, got %+v", result)
			}
			if result.CircleCI == nil || len(result.CircleCI.Workflows) != 1 || result.CircleCI.Workflows[0].JobsCompleted != tc.wantProof {
				t.Errorf("want jobsCompleted %v, got %+v", tc.wantProof, result.CircleCI)
			}
			if diff := cmp.Diff(prwait.MinInterval, clock.Now().Sub(started)); diff != "" {
				t.Errorf("must merge after exactly one poll interval (-want +got):\n%s", diff)
			}
			polls := 0
			for _, request := range cc.Requests() {
				if strings.HasSuffix(request.Path, "/workflow") {
					polls++
				}
			}
			if polls != 2 {
				t.Errorf("want two fresh workflow listings, got %d", polls)
			}
		})
	}
}
