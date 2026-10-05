package prmerge

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// The kinds of feedback the review guard reads.
const (
	FeedbackReview        = "review"
	FeedbackReviewComment = "review_comment"
	FeedbackComment       = "comment"
)

// maxNamedFeedback bounds the items the refusal's reason names; the
// document lists all of them.
const maxNamedFeedback = 5

// UnansweredReview is one piece of a reviewer's feedback the merge waits
// on: a review, a comment on the diff or a comment on the conversation,
// newer than the head and not answered since.
type UnansweredReview struct {
	// Kind is review, review_comment or comment.
	Kind string `json:"kind"`
	// Login is the reviewer.
	Login string `json:"login"`
	// State is the review's state, COMMENTED or CHANGES_REQUESTED; empty
	// for a comment.
	State string `json:"state,omitempty"`
	// At is when the review was submitted or the comment written.
	At time.Time `json:"at"`
	// URL links the review or the comment.
	URL string `json:"url"`
}

// feedback is the pull request's reviews and comments as the review guard
// reads them.
type feedback struct {
	reviews        []*github.PullRequestReview
	reviewComments []*github.PullRequestComment
	comments       []*github.IssueComment
	headSHA        string
	headAt         time.Time
}

// guardReviews refuses the merge (exit 5) while a person other than the
// pull request's author and the caller left feedback newer than the head
// that nobody answered. It reads the reviews, the review comments, the
// conversation's comments and the head commit's date afresh on every call:
// it runs before the wait and again right before the merge.
func (m *Merger) guardReviews(ctx context.Context, owner, repo string, number int, pr *github.PullRequest, caller string, result *Result) error {
	f := feedback{headSHA: pr.GetHead().GetSHA()}
	var err error
	if f.reviews, err = m.github.PullRequestReviews(ctx, owner, repo, number); err != nil {
		return microerror.Mask(err)
	}
	if f.reviewComments, err = m.github.PullRequestReviewComments(ctx, owner, repo, number); err != nil {
		return microerror.Mask(err)
	}
	if f.comments, err = m.github.IssueComments(ctx, owner, repo, number); err != nil {
		return microerror.Mask(err)
	}
	if f.headAt, err = m.github.CommitTime(ctx, owner, repo, f.headSHA); err != nil {
		return microerror.Mask(err)
	}

	unanswered := f.unanswered(pr.GetUser().GetLogin(), caller)
	result.UnansweredReviews = unanswered
	if len(unanswered) == 0 {
		m.progress.Printf("reviews: nothing unanswered on %s", f.headSHA)
		return nil
	}
	return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused, "%s", reviewRefusal(unanswered, answerers(pr.GetUser(), caller)))
}

// unanswered is the feedback that holds the merge, oldest first. It counts
// when its author is a person (no bot, GitHub App or automation account)
// other than the pull request's author and the caller; when it is newer
// than the head (on the head commit, or written after the head commit's
// date); and when it is a COMMENTED or CHANGES_REQUESTED review or a
// comment. It is answered by anything the author or the caller wrote on
// the pull request after it, or by an APPROVED review of the same reviewer
// no older than it.
func (f feedback) unanswered(author, caller string) []UnansweredReview {
	answerer := func(u *github.User) bool {
		return strings.EqualFold(u.GetLogin(), author) || strings.EqualFold(u.GetLogin(), caller)
	}
	reviewer := func(u *github.User) bool { return !answerer(u) && !isAutomation(u) }

	var lastAnswer time.Time
	answered := func(u *github.User, at time.Time) {
		if answerer(u) && at.After(lastAnswer) {
			lastAnswer = at
		}
	}
	approved := map[string]time.Time{}

	var items []UnansweredReview
	for _, r := range f.reviews {
		at := r.GetSubmittedAt().Time
		answered(r.GetUser(), at)
		if !reviewer(r.GetUser()) {
			continue
		}
		switch r.GetState() {
		case "APPROVED":
			login := strings.ToLower(r.GetUser().GetLogin())
			if at.After(approved[login]) {
				approved[login] = at
			}
		case "COMMENTED", "CHANGES_REQUESTED":
			if r.GetCommitID() == f.headSHA || at.After(f.headAt) {
				items = append(items, UnansweredReview{Kind: FeedbackReview, Login: r.GetUser().GetLogin(), State: r.GetState(), At: at, URL: r.GetHTMLURL()})
			}
		}
	}
	for _, c := range f.reviewComments {
		at := c.GetCreatedAt().Time
		answered(c.GetUser(), at)
		if reviewer(c.GetUser()) && (c.GetOriginalCommitID() == f.headSHA || at.After(f.headAt)) {
			items = append(items, UnansweredReview{Kind: FeedbackReviewComment, Login: c.GetUser().GetLogin(), At: at, URL: c.GetHTMLURL()})
		}
	}
	for _, c := range f.comments {
		at := c.GetCreatedAt().Time
		answered(c.GetUser(), at)
		if reviewer(c.GetUser()) && at.After(f.headAt) {
			items = append(items, UnansweredReview{Kind: FeedbackComment, Login: c.GetUser().GetLogin(), At: at, URL: c.GetHTMLURL()})
		}
	}

	open := []UnansweredReview{}
	for _, item := range items {
		if lastAnswer.After(item.At) {
			continue
		}
		if approval, ok := approved[strings.ToLower(item.Login)]; ok && !approval.Before(item.At) {
			continue
		}
		open = append(open, item)
	}
	slices.SortStableFunc(open, func(a, b UnansweredReview) int { return a.At.Compare(b.At) })
	return open
}

// answerers names who answers feedback: the author and the caller, or
// the caller alone on a bot's pull request, which cannot reply.
func answerers(author *github.User, caller string) string {
	switch {
	case isAutomation(author):
		return caller
	case strings.EqualFold(author.GetLogin(), caller):
		return caller
	default:
		return author.GetLogin() + " or " + caller
	}
}

// reviewRefusal is the reason of the review refusal (exit 5): how many
// items wait, the first few by kind, reviewer and link, and what clears
// them.
func reviewRefusal(open []UnansweredReview, answerers string) string {
	var named []string
	for _, item := range open[:min(len(open), maxNamedFeedback)] {
		what := strings.ReplaceAll(item.Kind, "_", " ")
		if item.State != "" {
			what += " " + item.State
		}
		named = append(named, fmt.Sprintf("%s by %s %s", what, item.Login, item.URL))
	}
	more := ""
	if len(open) > maxNamedFeedback {
		more = fmt.Sprintf(" and %d more", len(open)-maxNamedFeedback)
	}
	return fmt.Sprintf("unanswered review: %d item(s) newer than the head wait for an answer: %s%s; a reply from %s on the pull request, a new commit or the reviewer's approval clears them",
		len(open), strings.Join(named, ", "), more, answerers)
}
