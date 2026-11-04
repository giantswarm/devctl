package approvemergerenovate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/giantswarm/devctl/v8/internal/pr"
)

func TestPrintSummaryCountsTerminalFailures(t *testing.T) {
	for _, status := range []string{"Failed to get PR", "Failed checks", "Failed to get reviews", "Failed to approve", "Failed to get repo", "Merge conflicts", "Merge failed", "No merge methods"} {
		t.Run(status, func(t *testing.T) {
			for _, dryRun := range []bool{false, true} {
				var out bytes.Buffer
				r := &runner{flag: &flag{DryRun: dryRun}, stdout: &out}
				r.printSummary([]*pr.PRStatus{{Status: status}})
				if !strings.Contains(out.String(), "PRs failed: 1") {
					t.Errorf("dry-run=%t omitted failure %q: %s", dryRun, status, &out)
				}
				if dryRun && !strings.Contains(out.String(), "PRs that would be processed: 0") {
					t.Errorf("dry-run counted impossible operation %q: %s", status, &out)
				}
			}
		})
	}
}

func TestPrintSummaryPreservesOtherOutcomes(t *testing.T) {
	var out bytes.Buffer
	r := &runner{flag: &flag{}, stdout: &out}
	var statuses []*pr.PRStatus
	for _, status := range []string{"Merged (squash)", "Merged (auto-merge)", "Approved", "Queued to merge", "Already merged", "Auto-merge enabled", "Timeout waiting", "Waiting for checks (1/60)", "Merge failed", "No merge methods"} {
		statuses = append(statuses, &pr.PRStatus{Status: status})
	}
	r.printSummary(statuses)
	for _, want := range []string{"PRs merged: 2", "PRs approved: 1", "PRs queued to merge: 1", "PRs skipped: 2", "PRs failed: 2", "PRs still waiting: 2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q: %s", want, &out)
		}
	}
}
