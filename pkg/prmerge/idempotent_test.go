package prmerge_test

import (
	"context"
	"testing"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prmerge/prmergetest"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

// TestMergedAlready: a pull request merged before the call is exit 3 for the
// CLI, as it always was, and the merge's own outcome with Idempotent, the
// server's, with no merge call either way.
func TestMergedAlready(t *testing.T) {
	tests := map[string]struct {
		idempotent bool
		exit       int
	}{
		"the CLI":    {exit: agentcli.ExitNotApplicable},
		"idempotent": {idempotent: true, exit: agentcli.ExitOK},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pr := prmergetest.PullRequest("o", "r", 42, "abc123", prmergetest.Login)
			pr.State, pr.Merged = new("closed"), new(true)
			pr.MergeCommitSHA = new("m1")
			gh := prmergetest.New(pr)
			merger, err := prmerge.New(prmerge.Config{
				Wait:       prwait.Config{GitHub: gh, Clock: agentcli.NewVirtualClock(pr.GetUpdatedAt().Time)},
				Policy:     prmergetest.Policy(false),
				Idempotent: tt.idempotent,
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := merger.Merge(context.Background(), "o", "r", 42)
			if code := agentcli.Exit(err); code != tt.exit {
				t.Errorf("exit %d (%v), want %d", code, err, tt.exit)
			}
			if tt.idempotent && result.MergeCommitSHA != "m1" {
				t.Errorf("mergeCommitSha %q, want the merge's m1", result.MergeCommitSHA)
			}
			if n := len(gh.Merges()); n != 0 {
				t.Errorf("%d merge calls on a merged pull request", n)
			}
		})
	}
}
