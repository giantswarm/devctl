package prwait

import (
	"fmt"

	"github.com/google/go-github/v92/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// notApplicable is exit 3 for a pull request no wait can turn green: closed
// or merged, a draft, conflicting with its base, or behind a base whose
// protection requires it to be up to date. A mergeable state GitHub has not
// computed yet ("unknown") is not one of them: the wait goes on and reads the
// pull request again on the next poll.
func notApplicable(pr *github.PullRequest) error {
	reason := ""
	switch {
	case pr.GetMerged():
		reason = "the pull request is merged"
	case pr.GetState() != "open":
		reason = fmt.Sprintf("the pull request is %s", pr.GetState())
	case pr.GetDraft():
		reason = "the pull request is a draft"
	case pr.GetMergeableState() == "dirty":
		reason = fmt.Sprintf("the pull request conflicts with %s", pr.GetBase().GetRef())
	case pr.GetMergeableState() == "behind":
		reason = fmt.Sprintf("the pull request is behind %s, which requires branches to be up to date", pr.GetBase().GetRef())
	default:
		return nil
	}
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable, "%s", reason)
}

// isFork: the head lives in another repository than the base. A head whose
// repository GitHub no longer knows reads as the base's.
func isFork(pr *github.PullRequest) bool {
	headRepo, baseRepo := pr.GetHead().GetRepo(), pr.GetBase().GetRepo()
	if headRepo == nil || baseRepo == nil {
		return false
	}
	return headRepo.GetFullName() != baseRepo.GetFullName()
}
