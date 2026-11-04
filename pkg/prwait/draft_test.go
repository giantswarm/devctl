package prwait

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func Test_Wait_draftChecks(t *testing.T) {
	testCases := []struct {
		name         string
		initialDraft bool
		finalDraft   bool
		conclusion   string
		wantCode     int
	}{
		{name: "case 0: draft becomes green", initialDraft: true, finalDraft: true, conclusion: "success", wantCode: agentcli.ExitOK},
		{name: "case 1: draft becomes red", initialDraft: true, finalDraft: true, conclusion: "failure", wantCode: agentcli.ExitRed},
		{name: "case 2: draft stays pending", initialDraft: true, finalDraft: true, wantCode: agentcli.ExitTimeout},
		{name: "case 3: draft becomes ready", initialDraft: true, finalDraft: false, conclusion: "success", wantCode: agentcli.ExitOK},
		{name: "case 4: ready becomes draft", initialDraft: false, finalDraft: true, conclusion: "success", wantCode: agentcli.ExitOK},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := func(draft bool) sequence.Response {
				mergeable := "clean"
				if draft {
					mergeable = "draft"
				}
				return pull(mergeable, map[string]any{"draft": draft})
			}
			status := "completed"
			if tc.conclusion == "" {
				status = "in_progress"
			}
			gh := gitHubGreen()
			gh[pullPath] = []sequence.Response{state(tc.initialDraft), state(tc.finalDraft)}
			gh[runsPath] = []sequence.Response{
				checkRuns(run("plan-stages", "in_progress", "")),
				checkRuns(run("plan-stages", status, tc.conclusion)),
			}
			gh["GET /repos/o/r/branches/main/protection"] = []sequence.Response{
				body(map[string]any{"required_status_checks": map[string]any{"contexts": []string{"plan-stages"}}}),
			}
			h := start(t, gh, sequence.Routes{}, false, 45*time.Second)
			h.waiter.rate = nil

			result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
			if code := exitCode(err); code != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v", tc.wantCode, code, err)
			}
			if result.HeadSHA != sha || result.BaseRef != "main" || result.Draft != tc.finalDraft {
				t.Errorf("want the judged head and latest draft state %v, got %+v", tc.finalDraft, result)
			}
			if len(result.Checks) != 1 || result.Checks[0].Conclusion != tc.conclusion || !result.Checks[0].Required {
				t.Errorf("want the latest required plan-stages check, got %+v", result.Checks)
			}
			if !strings.Contains(h.progress.String(), "poll 1: waiting for check plan-stages (in_progress)") || !strings.Contains(h.progress.String(), "poll 2:") {
				t.Errorf("must wait past the pending check:\n%s", h.progress.String())
			}

			doc := NewDocument(time.Now())
			doc.Take(result)
			data, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if draft, _ := fields["draft"].(bool); draft != tc.finalDraft {
				t.Errorf("JSON must report draft: %v, got %s", tc.finalDraft, data)
			}
		})
	}
}
