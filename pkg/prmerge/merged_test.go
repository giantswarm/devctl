package prmerge

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func Test_Merge_requiredReviewWarningAfterMerge(t *testing.T) {
	const missing = "an approving review the base requires is missing"
	const pending = "status external/scan (pending, not required, not waited for)"
	tests := []struct {
		name       string
		decision   string
		configure  func(*Config)
		responses  sequence.Routes
		queued     bool
		wantCode   int
		wantMerged bool
		wantReview string
	}{
		{name: "bypass", decision: "REVIEW_REQUIRED", wantMerged: true, wantReview: "merged through the ruleset bypass: " + missing},
		{
			name: "branch cleanup fails after merging", decision: "REVIEW_REQUIRED", wantCode: -1,
			responses:  sequence.Routes{"DELETE /repos/o/r/git/refs/heads/feature": {{Status: http.StatusForbidden, Body: map[string]any{"message": "forbidden"}}}},
			wantMerged: true, wantReview: "merged through the ruleset bypass: " + missing,
		},
		{
			name: "release fails after merging", decision: "REVIEW_REQUIRED", wantCode: agentcli.ExitReleaseFailed,
			configure: func(c *Config) {
				var calls []releaseCall
				c.Release = fakeRelease(&calls, "v1.2.3", agentcli.NewExitError(agentcli.ExitRed, agentcli.VerdictCIFailed, "release pipeline failed"))
			},
			wantMerged: true, wantReview: "merged through the ruleset bypass: " + missing,
		},
		{
			name: "GitHub declines the merge", decision: "REVIEW_REQUIRED", wantCode: agentcli.ExitNotApplicable,
			responses:  sequence.Routes{"PUT /repos/o/r/pulls/42/merge": {{Status: http.StatusMethodNotAllowed, Body: map[string]any{"message": "At least 1 approving review is required by reviewers with write access."}}}},
			wantReview: "GitHub blocks the merge: " + missing,
		},
		{name: "approved", decision: "APPROVED", wantMerged: true},
		{
			name: "queue confirms the merge", decision: "REVIEW_REQUIRED", queued: true, wantMerged: true,
			wantReview: "merged through the merge queue: the wait reported a missing required approval",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := routes(pull(map[string]any{"mergeable_state": "blocked"}), sequence.Routes{
				"POST /graphql": {{Body: map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewDecision": tc.decision}}}}}},
				"GET /repos/o/r/commits/abc123/status": {{Body: map[string]any{"state": "pending", "total_count": 1, "statuses": []any{
					map[string]any{"context": "external/scan", "state": "pending", "target_url": "https://ci/scan"},
				}}}},
			}, tc.responses)
			h := newHarness(t, r, tc.configure)
			if tc.queued {
				h.merger.github = &reviewQueueGitHub{GitHub: h.merger.github}
			}
			result, err := h.merge(t)
			if code := exitCode(err); code != tc.wantCode {
				t.Fatalf("want exit %d, got %d: %v\n%s", tc.wantCode, code, err, h.progress.String())
			}
			if (result.MergeCommitSHA != "") != tc.wantMerged || (result.MergedBy != "") != tc.wantMerged {
				t.Fatalf("merge outcome: sha %q by %q", result.MergeCommitSHA, result.MergedBy)
			}
			wantBlocking := []string{pending}
			if !tc.wantMerged {
				wantBlocking = append(wantBlocking, "review: "+missing)
			}
			if !slices.Equal(result.Blocking, wantBlocking) {
				t.Errorf("blocking: want %q, got %q", wantBlocking, result.Blocking)
			}
			wantWarnings := []string{"pending: " + pending}
			if tc.wantReview != "" {
				wantWarnings = append(wantWarnings, tc.wantReview)
			}
			if !slices.Equal(result.Warnings, wantWarnings) {
				t.Errorf("warnings: want %q, got %q", wantWarnings, result.Warnings)
			}
			if tc.wantMerged && tc.wantReview != "" && !strings.Contains(h.progress.String(), "warning: "+tc.wantReview) {
				t.Errorf("progress did not explain the merge: %s", h.progress.String())
			}

			// CLI and MCP both take the engine result into this document.
			doc := NewDocument(time.Now(), result.Method)
			doc.Take(result)
			doc.Finish(time.Now(), agentcli.VerdictGreen, err)
			data, marshalErr := json.Marshal(doc)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var document struct {
				Warnings []string `json:"warnings"`
				Blocking []string `json:"blocking"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(document.Warnings, wantWarnings) || !slices.Equal(document.Blocking, wantBlocking) {
				t.Errorf("document diagnostics: %s", data)
			}
		})
	}
}

// reviewQueueGitHub confirms a merge only after enqueueing, independently
// of how many reads the shared wait makes before then.
type reviewQueueGitHub struct {
	GitHub
	enqueued bool
}

func (g *reviewQueueGitHub) MergeQueueRequired(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func (g *reviewQueueGitHub) EnqueuePullRequest(context.Context, string) error {
	g.enqueued = true
	return nil
}

func (g *reviewQueueGitHub) PullRequest(ctx context.Context, owner, repo string, number int) (*github.PullRequest, error) {
	if !g.enqueued {
		return g.GitHub.PullRequest(ctx, owner, repo, number)
	}
	return &github.PullRequest{Merged: new(true), MergeCommitSHA: new("q1"), MergedBy: &github.User{Login: new("queue-enqueuer")}}, nil
}
