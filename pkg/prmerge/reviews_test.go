package prmerge

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

func at(minute int) *github.Timestamp {
	return &github.Timestamp{Time: time.Date(2026, 10, 1, 10, minute, 0, 0, time.UTC)}
}

func person(login string) *github.User { return &github.User{Login: &login, Type: ptr("User")} }

func review(login, state, commit string, minute int) *github.PullRequestReview {
	return &github.PullRequestReview{User: person(login), State: &state, CommitID: &commit, SubmittedAt: at(minute), HTMLURL: ptr("https://github.com/o/r/pull/42#review")}
}

func reviewComment(login, commit string, minute int) *github.PullRequestComment {
	return &github.PullRequestComment{User: person(login), OriginalCommitID: &commit, CreatedAt: at(minute), HTMLURL: ptr("https://github.com/o/r/pull/42#discussion")}
}

func comment(user *github.User, minute int) *github.IssueComment {
	return &github.IssueComment{User: user, CreatedAt: at(minute), HTMLURL: ptr("https://github.com/o/r/pull/42#issuecomment")}
}

func Test_feedback_unanswered(t *testing.T) {
	// The head abc123 is committed at 10:10; old is the commit before it.
	tests := []struct {
		name string
		f    feedback
		want []string // kind/login of what holds the merge
	}{
		{name: "nothing", want: nil},
		{name: "a COMMENTED review on the head holds",
			f:    feedback{reviews: []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12)}},
			want: []string{"review/marians"}},
		{name: "a CHANGES_REQUESTED review on the head holds",
			f:    feedback{reviews: []*github.PullRequestReview{review("marians", "CHANGES_REQUESTED", "abc123", 12)}},
			want: []string{"review/marians"}},
		{name: "a review of an older commit was answered by the head",
			f: feedback{reviews: []*github.PullRequestReview{review("marians", "CHANGES_REQUESTED", "old", 5)}}},
		{name: "a review with inline comments holds with each comment",
			f: feedback{
				reviews:        []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12)},
				reviewComments: []*github.PullRequestComment{reviewComment("marians", "abc123", 12), reviewComment("marians", "abc123", 12)},
			},
			want: []string{"review/marians", "review_comment/marians", "review_comment/marians"}},
		{name: "the author's reply in a thread answers",
			f: feedback{
				reviews:        []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12)},
				reviewComments: []*github.PullRequestComment{reviewComment("marians", "abc123", 12), reviewComment("someone", "abc123", 14)},
			}},
		{name: "the author's comment on the conversation answers",
			f: feedback{
				reviews:  []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12)},
				comments: []*github.IssueComment{comment(person("someone"), 13)},
			}},
		{name: "the author's earlier comment answers nothing",
			f: feedback{
				reviews:  []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12)},
				comments: []*github.IssueComment{comment(person("someone"), 11)},
			},
			want: []string{"review/marians"}},
		{name: "the caller's reply answers on a bot's pull request",
			f: feedback{
				reviews:  []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12)},
				comments: []*github.IssueComment{comment(person("Caller"), 13)},
			}},
		{name: "the reviewer's approval after the review clears it",
			f: feedback{reviews: []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12), review("marians", "APPROVED", "abc123", 15)}}},
		{name: "another reviewer's approval clears nothing",
			f:    feedback{reviews: []*github.PullRequestReview{review("marians", "COMMENTED", "abc123", 12), review("piontec", "APPROVED", "abc123", 15)}},
			want: []string{"review/marians"}},
		{name: "an approval before the review clears nothing",
			f:    feedback{reviews: []*github.PullRequestReview{review("marians", "APPROVED", "abc123", 11), review("marians", "CHANGES_REQUESTED", "abc123", 12)}},
			want: []string{"review/marians"}},
		{name: "a dismissed review and an approval hold nothing",
			f: feedback{reviews: []*github.PullRequestReview{review("marians", "DISMISSED", "abc123", 12), review("piontec", "APPROVED", "abc123", 13)}}},
		{name: "a person's comment on the conversation after the head holds",
			f:    feedback{comments: []*github.IssueComment{comment(person("marians"), 12)}},
			want: []string{"comment/marians"}},
		{name: "a comment before the head was answered by the head",
			f: feedback{comments: []*github.IssueComment{comment(person("marians"), 5)}}},
		{name: "bots, apps and automation accounts never hold",
			f: feedback{comments: []*github.IssueComment{
				comment(&github.User{Login: ptr("renovate[bot]"), Type: ptr("Bot")}, 12),
				comment(&github.User{Login: ptr("some-app"), Type: ptr("Bot")}, 12),
				comment(&github.User{Login: ptr("taylorbot"), Type: ptr("User"), ID: ptr(int64(25685558))}, 12),
			}}},
		{name: "an impostor of an automation login holds",
			f:    feedback{comments: []*github.IssueComment{comment(&github.User{Login: ptr("taylorbot"), Type: ptr("User"), ID: ptr(int64(1))}, 12)}},
			want: []string{"comment/taylorbot"}},
		{name: "the author's own comments never hold",
			f: feedback{comments: []*github.IssueComment{comment(person("someone"), 12)}}},
		{name: "a review comment on an older commit written after the head holds",
			f:    feedback{reviewComments: []*github.PullRequestComment{reviewComment("marians", "old", 12)}},
			want: []string{"review_comment/marians"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.f.headSHA, tc.f.headAt = "abc123", at(10).Time
			var got []string
			for _, item := range tc.f.unanswered("someone", "caller") {
				got = append(got, item.Kind+"/"+item.Login)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

// commented is a COMMENTED review of the head abc123 by marians, after the
// head's commit date.
var commented = map[string]any{
	"id": 7, "state": "COMMENTED", "commit_id": "abc123", "submitted_at": "2026-10-01T10:05:00Z",
	"user":     map[string]any{"login": "marians", "type": "User"},
	"html_url": "https://github.com/o/r/pull/42#pullrequestreview-7",
}

func Test_Merge_unansweredReviewRefusesBeforeTheWait(t *testing.T) {
	h := newHarness(t, routes(pull(nil), sequence.Routes{
		"GET /repos/o/r/pulls/42/reviews": {{Body: []any{commented}}},
	}), nil)

	result, err := h.merge(t)
	if exitCode(err) != agentcli.ExitRefused {
		t.Fatalf("want exit 5, got %v", err)
	}
	if !strings.Contains(err.Error(), "unanswered review: 1 item(s)") || !strings.Contains(err.Error(), "review COMMENTED by marians https://github.com/o/r/pull/42#pullrequestreview-7") {
		t.Errorf("reason: %v", err)
	}
	if len(result.UnansweredReviews) != 1 || result.UnansweredReviews[0].State != "COMMENTED" {
		t.Errorf("unansweredReviews: %+v", result.UnansweredReviews)
	}
	if n := h.requested("GET /repos/o/r/commits/abc123/check-runs"); n != 0 {
		t.Errorf("the wait ran: %d check-run reads", n)
	}
	if n := h.requested("PUT /repos/o/r/pulls/42/merge"); n != 0 {
		t.Errorf("merged %d times", n)
	}
}

func Test_Merge_reviewDuringTheWaitRefusesTheMerge(t *testing.T) {
	h := newHarness(t, routes(pull(nil), sequence.Routes{
		"GET /repos/o/r/pulls/42/reviews": {{Body: []any{}}, {Body: []any{commented}}},
	}), nil)

	_, err := h.merge(t)
	if exitCode(err) != agentcli.ExitRefused || !strings.Contains(err.Error(), "unanswered review") {
		t.Fatalf("want exit 5 for the review, got %v", err)
	}
	if n := h.requested("GET /repos/o/r/commits/abc123/check-runs"); n == 0 {
		t.Error("the wait did not run")
	}
	if n := h.requested("PUT /repos/o/r/pulls/42/merge"); n != 0 {
		t.Errorf("merged %d times", n)
	}
}

func Test_Merge_answeredReviewMerges(t *testing.T) {
	reply := map[string]any{
		"id": 9, "created_at": "2026-10-01T10:06:00Z", "user": map[string]any{"login": "someone", "type": "User"},
		"html_url": "https://github.com/o/r/pull/42#issuecomment-9",
	}
	h := newHarness(t, routes(pull(nil), sequence.Routes{
		"GET /repos/o/r/pulls/42/reviews":   {{Body: []any{commented}}},
		"GET /repos/o/r/issues/42/comments": {{Body: []any{reply}}},
	}), nil)

	result, err := h.merge(t)
	if err != nil {
		t.Fatalf("want a merge, got %v", err)
	}
	if result.MergeCommitSHA != "m1" || len(result.UnansweredReviews) != 0 {
		t.Errorf("result: %+v", result)
	}
}

func ptr[T any](v T) *T { return &v }
