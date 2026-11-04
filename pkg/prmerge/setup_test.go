package prmerge

import (
	"context"
	"testing"
	"time"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func Test_Merge_haltedSetup(t *testing.T) {
	stopped := time.Date(2026, time.October, 12, 7, 20, 0, 0, time.UTC)
	testCases := []struct {
		name         string
		check        string
		missing      bool
		continuation bool
		wantCode     int
	}{
		{name: "case 0: halted setup merges once the head is green", check: "success"},
		{name: "case 1: a failed head check prevents merging", check: "failure", wantCode: agentcli.ExitRed},
		{name: "case 2: an absent required context prevents merging", check: "success", missing: true, wantCode: agentcli.ExitRequiredMissing},
		{name: "case 3: a late failed continuation prevents merging", check: "success", continuation: true, wantCode: agentcli.ExitRed},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pipeline := func(state string) sequence.Response {
				return sequence.Response{Body: map[string]any{"items": []any{map[string]any{
					"id": "p1", "number": 12, "state": state, "vcs": map[string]any{"revision": "abc123", "branch": "feature"},
				}}}}
			}
			setup := map[string]any{"id": "w1", "name": "pr-retag-setup", "tag": "setup", "status": "success", "stopped_at": stopped}
			cc := sequence.Routes{
				"GET /api/v2/project/gh/o/r":                         {{Body: map[string]any{"slug": "gh/o/r"}}},
				"GET /api/v2/project/gh/o/r/pipeline":                {pipeline("setup")},
				"GET /api/v2/project/gh/o/r/pipeline?branch=feature": {pipeline("setup")},
				"GET /api/v2/pipeline/p1/workflow":                   {{Body: map[string]any{"items": []any{setup}}}},
				"GET /api/v2/workflow/w1/job": {{Body: map[string]any{"items": []any{
					map[string]any{"name": "setup-retag", "status": "success"},
				}}}},
			}
			if tc.continuation {
				cc["GET /api/v2/project/gh/o/r/pipeline?branch=feature"] = []sequence.Response{pipeline("setup"), pipeline("created")}
				cc["GET /api/v2/pipeline/p1/workflow"] = append(cc["GET /api/v2/pipeline/p1/workflow"], sequence.Response{Body: map[string]any{"items": []any{
					setup, map[string]any{"id": "w2", "name": "build", "status": "failed"},
				}}})
			}
			server, err := circlemock.Start(cc)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.Close)
			gh := routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/contents/.circleci/config.yml": {{Body: map[string]any{"type": "file", "content": "", "encoding": "base64"}}},
				"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
					map[string]any{"id": 1, "name": "lint", "status": "completed", "conclusion": tc.check},
				}}}},
				"GET /repos/o/r/commits/abc123/status": {{Body: map[string]any{"state": "success", "total_count": 1, "statuses": []any{
					map[string]any{"context": "ci/circleci: setup-retag", "state": "success"},
				}}}},
			})
			if tc.missing {
				gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{{Body: map[string]any{
					"required_status_checks": map[string]any{"contexts": []any{"ci/circleci: build"}},
				}}}
			}
			clock := agentcli.NewVirtualClock(stopped)
			h := newHarness(t, gh, func(c *Config) {
				c.Wait.Clock, c.Wait.Rate, c.Wait.Timeout = clock, nil, 6*time.Minute
				c.Wait.CircleCI = func(context.Context) (*circleciclient.Client, error) {
					return circleciclient.New(circleciclient.Config{Anonymous: true, BaseURL: server.URL})
				}
			})
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, code, err)
			}
			merges := h.requested("PUT /repos/o/r/pulls/42/merge")
			if tc.wantCode == 0 {
				if merges != 1 || result.MergeCommitSHA != "m1" || result.HeadSHA != "abc123" || result.CircleCI == nil || !result.CircleCI.SetupHalted {
					t.Errorf("want one merge of the halted setup's green head, got %d merges, result %+v", merges, result)
				}
				if elapsed := clock.Now().Sub(stopped); elapsed < 5*time.Minute || elapsed > 5*time.Minute+time.Minute {
					t.Errorf("want a merge after the bounded continuation grace, got %s", elapsed)
				}
			} else if merges != 0 || h.requested("DELETE /repos/o/r/git/refs/heads/feature") != 0 {
				t.Errorf("a head without a green verdict must not merge or delete its branch")
			}
		})
	}
}
