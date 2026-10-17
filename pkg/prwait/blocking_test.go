package prwait

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// gatekeeper is a check run of another app than GitHub Actions that stays in
// progress while the pull request lacks its review.
func gatekeeper() map[string]any {
	r := run("Heimdall - PR Gatekeeper", "in_progress", "")
	r["id"] = 9
	r["html_url"] = "https://github.com/o/r/runs/9"
	r["app"] = map[string]any{"slug": "tinkerers-ci"}
	return r
}

// requiresLint is a base whose ruleset requires the context lint.
func requiresLint(gh sequence.Routes) {
	gh["GET /repos/o/r/rules/branches/main"] = []sequence.Response{body([]any{
		map[string]any{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": []any{map[string]any{"context": "lint"}}}},
	})}
}

// reviewDecision is the GraphQL answer naming the pull request's review
// decision.
func reviewDecision(decision string) sequence.Response {
	return body(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewDecision": decision}}}})
}

func Test_Wait_pendingNonRequiredCheckIsNotWaitedFor(t *testing.T) {
	// The required check is green and the only pending check is not
	// required: green at the first poll, the pending check named.
	gh := gitHubGreen()
	gh[runsPath] = []sequence.Response{checkRuns(run("lint", "completed", "success"), gatekeeper())}
	requiresLint(gh)
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if want := []string{"check Heimdall - PR Gatekeeper (in_progress, not required, not waited for)"}; !cmp.Equal(want, result.Blocking) {
		t.Errorf("blocking: want %q, got %q", want, result.Blocking)
	}
	if polls := strings.Count(h.progress.String(), "poll "); polls != 1 {
		t.Errorf("want one poll, got %d:\n%s", polls, h.progress.String())
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "Heimdall - PR Gatekeeper") {
		t.Errorf("want one warning naming the pending check, got %q", result.Warnings)
	}
}

func Test_Wait_pendingRequiredCheckIsWaitedFor(t *testing.T) {
	// A required check of another app than GitHub Actions is waited for.
	gh := gitHubGreen()
	pending := gatekeeper()
	done := gatekeeper()
	done["status"], done["conclusion"] = "completed", "success"
	gh[runsPath] = []sequence.Response{checkRuns(pending), checkRuns(done)}
	gh["GET /repos/o/r/rules/branches/main"] = []sequence.Response{body([]any{
		map[string]any{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": []any{map[string]any{"context": "Heimdall - PR Gatekeeper"}}}},
	})}
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if !strings.Contains(h.progress.String(), "poll 1: waiting for check Heimdall - PR Gatekeeper (in_progress)") {
		t.Errorf("want the required check waited for:\n%s", h.progress.String())
	}
	if len(result.Blocking) != 0 {
		t.Errorf("blocking: want none, got %q", result.Blocking)
	}
}

func Test_Wait_pendingNonRequiredStatusIsNotWaitedFor(t *testing.T) {
	gh := gitHubGreen()
	gh[statPath] = []sequence.Response{body(map[string]any{"state": "pending", "total_count": 1, "statuses": []any{
		map[string]any{"context": "external/scan", "state": "pending", "target_url": "https://ci/scan", "updated_at": "2026-09-21T10:00:00Z"},
	}})}
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if want := []string{"status external/scan (pending, not required, not waited for)"}; !cmp.Equal(want, result.Blocking) {
		t.Errorf("blocking: want %q, got %q", want, result.Blocking)
	}
}

func Test_Wait_missingReviewIsNamed(t *testing.T) {
	// The checks are green and GitHub blocks the merge for a missing
	// review: green, and the result names the review.
	gh := gitHubGreen()
	gh[pullPath] = []sequence.Response{pull("blocked", nil)}
	gh[runsPath] = []sequence.Response{checkRuns(run("lint", "completed", "success"), gatekeeper())}
	gh[graphQLPath] = []sequence.Response{reviewDecision("REVIEW_REQUIRED")}
	requiresLint(gh)
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	want := []string{
		"check Heimdall - PR Gatekeeper (in_progress, not required, not waited for)",
		"review: an approving review the base requires is missing",
	}
	if diff := cmp.Diff(want, result.Blocking); diff != "" {
		t.Errorf("blocking (-want +got):\n%s", diff)
	}
}

func Test_Wait_changesRequestedIsNamed(t *testing.T) {
	gh := gitHubGreen()
	gh[pullPath] = []sequence.Response{pull("blocked", nil)}
	gh[graphQLPath] = []sequence.Response{reviewDecision("CHANGES_REQUESTED")}
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if want := []string{"review: a reviewer requested changes"}; !cmp.Equal(want, result.Blocking) {
		t.Errorf("blocking: want %q, got %q", want, result.Blocking)
	}
}

func Test_Wait_unreadableReviewDecisionKeepsTheVerdict(t *testing.T) {
	gh := gitHubGreen()
	gh[pullPath] = []sequence.Response{pull("blocked", nil)}
	gh[graphQLPath] = []sequence.Response{{Status: 502, Body: map[string]any{"message": "bad gateway"}}}
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	result, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if len(result.Blocking) != 0 {
		t.Errorf("blocking: want none, got %q", result.Blocking)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "review decision") {
		t.Errorf("want one warning naming the unread review decision, got %q", result.Warnings)
	}
}

func Test_Wait_onlyANonRequiredCheckStillExpectsTheRuns(t *testing.T) {
	// Right after a push only the gatekeeper has reported: the head's
	// pull_request runs are not registered yet, and the wait goes on for
	// them instead of calling the head green.
	gh := emptyHead(time.Now(), "fix", "  pull_request:")
	gh[runsPath] = []sequence.Response{checkRuns(gatekeeper()), checkRuns(gatekeeper(), run("test", "completed", "success"))}
	h := start(t, gh, sequence.Routes{}, false, 30*time.Minute)

	_, err := h.waiter.Wait(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("want green, got %d %v", exitCode(err), err)
	}
	if !strings.Contains(h.progress.String(), "poll 1: waiting for actions runs of .github/workflows/ci.yaml (none registered)") {
		t.Errorf("want the runs expected at poll 1:\n%s", h.progress.String())
	}
	if exitCode(err) != agentcli.ExitOK {
		t.Errorf("want exit 0, got %d", exitCode(err))
	}
}
