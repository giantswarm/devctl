package wait

import (
	"testing"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/prwait/prwaittest"
)

func Test_run_draft(t *testing.T) {
	testCases := []struct {
		name        string
		conclusion  string
		wantCode    float64
		wantVerdict string
	}{
		{name: "case 0: green draft", conclusion: "success", wantCode: 0, wantVerdict: "green"},
		{name: "case 1: red draft", conclusion: "failure", wantCode: 1, wantVerdict: "red"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pr := prwaittest.OpenPullRequest(7, "abc123", time.Now().Add(-time.Hour))
			pr.Draft = new(true)
			gh := &prwaittest.GitHub{
				PR:       pr,
				Required: []string{"plan-stages"},
				Heads: []prwaittest.Head{
					{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("plan-stages", "in_progress", "")}},
					{CheckRuns: []*github.CheckRun{prwaittest.CheckRun("plan-stages", "completed", tc.conclusion)}},
				},
			}
			doc := cliDocument(t, gh)
			if doc["draft"] != true || doc["exitCode"] != tc.wantCode || doc["verdict"] != tc.wantVerdict {
				t.Errorf("want draft true, exit %v and %s, got %v", tc.wantCode, tc.wantVerdict, doc)
			}
			if polls := gh.Polls(); polls < 2 {
				t.Errorf("must wait for the draft's pending check, got %d poll(s)", polls)
			}
		})
	}
}
