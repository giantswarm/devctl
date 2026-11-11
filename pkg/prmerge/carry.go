package prmerge

import (
	"context"
	"fmt"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
)

// Carry identifies the source of a carry without replacing any commit's
// author. Rebase preserves the commits' original Git author identities.
type Carry struct {
	PullRequest string `json:"pullRequest"`
	Author      string `json:"author"`
}

// guardCarry runs before the wait (and any branch update) and again before
// the merge. The comparison uses immutable SHAs so a push during pagination
// cannot substitute commits for those of the judged head.
func (m *Merger) guardCarry(ctx context.Context, owner, repo string, number int, pr *github.PullRequest, result *Result, queued bool) error {
	if m.carryOf == 0 {
		return nil
	}
	refuse := func(reason string) error {
		return agentcli.NewExitError(agentcli.ExitRefused, agentcli.VerdictRefused, "--carry-of: %s", reason)
	}
	if m.carryOf == number {
		return refuse("the original pull request must differ from the carrier")
	}
	if queued {
		return refuse("a merge queue chooses its own merge method; a carry requires rebase to preserve authorship")
	}
	if pr.GetHead().GetSHA() != result.HeadSHA {
		return refuse("the head moved after its checks were judged; run the merge again")
	}
	if pr.GetBase().GetSHA() == "" || result.HeadSHA == "" {
		return refuse("the base and head commit SHAs must be known to verify authorship")
	}
	original, err := m.github.PullRequest(ctx, owner, repo, m.carryOf)
	if err != nil {
		return microerror.Mask(err)
	}
	author := original.GetUser()
	if author.GetID() == 0 || author.GetLogin() == "" {
		return refuse("the original pull request's author could not be identified")
	}
	result.CarryOf = &Carry{
		PullRequest: fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, m.carryOf),
		Author:      author.GetLogin(),
	}
	commits, err := m.github.CommitsBetween(ctx, owner, repo, pr.GetBase().GetSHA(), result.HeadSHA)
	if err != nil {
		return microerror.Mask(err)
	}
	if len(commits) == 0 {
		return refuse("no carried commits could be verified")
	}
	for _, commit := range commits {
		if commit.GetAuthor().GetID() != author.GetID() || commit.GetCommit().GetAuthor().GetName() == "" || commit.GetCommit().GetAuthor().GetEmail() == "" {
			return refuse(fmt.Sprintf("commit %s is not verifiably authored by %s, the author of %s", commit.GetSHA(), author.GetLogin(), result.CarryOf.PullRequest))
		}
	}
	m.progress.Printf("carry: preserving %s's authorship from %s through rebase", author.GetLogin(), result.CarryOf.PullRequest)
	return nil
}
