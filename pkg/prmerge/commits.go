package prmerge

import (
	"context"
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
)

// guardCommits refuses leftover autosquash commits when the selected method
// preserves each commit. It reads the initial head before waiting, then the
// judged head before merging: a push or branch update must be checked too.
func (m *Merger) guardCommits(ctx context.Context, owner, repo string, pr *github.PullRequest, result *Result) error {
	if result.Method != string(githubclient.MergeRebase) {
		return nil
	}
	commits, err := m.github.CommitsBetween(ctx, owner, repo, pr.GetBase().GetSHA(), result.HeadSHA)
	if err != nil {
		return microerror.Mask(err)
	}
	var pending []string
	for _, commit := range commits {
		subject, _, _ := strings.Cut(commit.GetCommit().GetMessage(), "\n")
		if strings.HasPrefix(subject, "amend!") || strings.HasPrefix(subject, "fixup!") || strings.HasPrefix(subject, "squash!") {
			pending = append(pending, fmt.Sprintf("%s %s", commit.GetSHA(), subject))
		}
	}
	if len(pending) == 0 {
		return nil
	}
	return agentcli.NewExitError(agentcli.ExitNotApplicable, agentcli.VerdictNotApplicable,
		"rebase merge would preserve unsquashed autosquash commits: %s; autosquash them and run the merge again; --accept-red does not override commit history", strings.Join(pending, "; "))
}
