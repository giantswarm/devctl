package prmerge

import (
	"context"
	"strings"
	"testing"
	"time"

	circlemock "github.com/giantswarm/devctl/v8/e2e/mock/circleci"
	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/circleciclient"
)

func Test_Merge_noCircleCIPipelines(t *testing.T) {
	for _, tc := range []struct {
		name       string
		conclusion string
		baseBuilt  bool
		wantCode   int
		wantMerged bool
	}{
		{name: "green merges", conclusion: "success", wantMerged: true},
		{name: "red refuses", conclusion: "failure", wantCode: agentcli.ExitRed},
		{name: "base built waits for head", conclusion: "success", baseBuilt: true, wantCode: agentcli.ExitTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := routes(pull(nil), sequence.Routes{
				"GET /repos/o/r/contents/.circleci/config.yml": {{Body: map[string]any{"type": "file", "content": "", "encoding": "base64"}}},
				"GET /repos/o/r/commits/abc123/check-runs": {{Body: map[string]any{"total_count": 1, "check_runs": []any{
					map[string]any{"id": 1, "name": "test", "status": "completed", "conclusion": tc.conclusion},
				}}}},
			})
			none := sequence.Response{Body: map[string]any{"items": []any{}}}
			listing := none
			if tc.baseBuilt {
				listing = sequence.Response{Body: map[string]any{"items": []any{map[string]any{"id": "base", "vcs": map[string]any{"revision": "base123", "branch": "main"}}}}}
			}
			cc, err := circlemock.Start(sequence.Routes{
				"GET /api/v2/project/gh/o/r":                         {{Body: map[string]any{"slug": "gh/o/r", "name": "r"}}},
				"GET /api/v2/project/gh/o/r/pipeline?branch=feature": {none},
				"GET /api/v2/project/gh/o/r/pipeline":                {listing},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cc.Close)
			h := newHarness(t, r, func(c *Config) {
				c.Wait.Clock = agentcli.NewVirtualClock(time.Now())
				c.Wait.Rate = nil
				c.Wait.Timeout = time.Minute
				c.Wait.CircleCI = func(context.Context) (*circleciclient.Client, error) {
					return circleciclient.New(circleciclient.Config{Token: "cci_test", BaseURL: cc.URL})
				}
			})
			result, err := h.merge(t)
			if exitCode(err) != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, exitCode(err), err)
			}
			wantCalls := 0
			if tc.wantMerged {
				wantCalls = 1
				if result.MergeCommitSHA != "m1" || !result.BranchDeleted {
					t.Errorf("want confirmed merge and branch cleanup, got %+v", result)
				}
			}
			if calls := h.requested("PUT /repos/o/r/pulls/42/merge"); calls != wantCalls {
				t.Errorf("want %d merge calls, got %d", wantCalls, calls)
			}
			if !tc.baseBuilt && (len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "no listed CircleCI pipelines")) {
				t.Errorf("want absent-pipeline warning in merge result, got %q", result.Warnings)
			}
		})
	}
}
