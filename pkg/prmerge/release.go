package prmerge

import (
	"context"
	"fmt"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

// ReleaseWait waits for the release a merge triggered, the wait of devctl
// release wait --pr given the merge commit, and fills result as far as it
// got. The error is the release wait's outcome in its own exit-code table.
type ReleaseWait func(ctx context.Context, owner, repo string, number int, mergeCommitSHA string, result *releasewait.Result) error

// Release is the release the merge triggered as devctl release wait --pr
// reports it: its verdict and reason, and its result.
type Release struct {
	// Verdict is the release wait's: available, no_release (none follows
	// the merge), ci_failed, timeout, not_applicable, usage, auth_required.
	Verdict agentcli.Verdict `json:"verdict"`
	// Reason is the release wait's reason; empty when available.
	Reason string `json:"reason"`
	releasewait.Result
}

// awaitRelease runs the release wait after the merge and returns the
// merge's outcome: nil when the release is available or none follows the
// merge, exit 6 when the release failed, exit 9 when it was not confirmed.
func (m *Merger) awaitRelease(ctx context.Context, owner, repo string, number int, result *Result) error {
	release := &Release{Result: releasewait.NewResult(result.Repository)}
	result.Release = release
	m.progress.Printf("release: waiting for the release of %s", result.MergeCommitSHA)

	err := m.release(ctx, owner, repo, number, result.MergeCommitSHA, &release.Result)
	if err == nil {
		release.Verdict = agentcli.VerdictAvailable
		m.progress.Printf("release: %s available", release.Tag)
		return nil
	}
	code, verdict := agentcli.Outcome(err)
	release.Verdict, release.Reason = verdict, err.Error()
	switch {
	case releasewait.IsNoRelease(err):
		m.progress.Printf("release: none follows the merge: %s", err)
		return nil
	case code == agentcli.ExitRed:
		return agentcli.NewExitError(agentcli.ExitReleaseFailed, agentcli.VerdictReleaseFailed, "merged as %s, and the release failed: %s", result.MergeCommitSHA, err)
	}
	resume := ""
	if verdict == agentcli.VerdictTimeout || verdict == agentcli.VerdictAuthRequired {
		resume = fmt.Sprintf("; devctl release wait %s --pr %d resumes the wait", result.Repository, number)
	}
	return agentcli.NewExitError(agentcli.ExitReleaseUnconfirmed, agentcli.VerdictReleaseUnconfirmed, "merged as %s, and the release is not confirmed: %s%s", result.MergeCommitSHA, err, resume)
}
