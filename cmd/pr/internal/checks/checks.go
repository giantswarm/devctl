// Package checks reads the independent check sources of the bulk PR commands.
package checks

import (
	"context"
	"fmt"

	"github.com/google/go-github/v93/github"
)

// Result includes the listings used to explain missing required contexts.
type Result struct {
	Combined *github.CombinedStatus
	Runs     *github.ListCheckRunsResults
	Failed   bool
	Pending  bool
}

// Read requires both sources to be readable, including every listing page.
// A green commit status says nothing about the separate check-run source.
func Read(ctx context.Context, client *github.Client, owner, repo, sha string) (Result, error) {
	result := Result{Combined: &github.CombinedStatus{}, Runs: &github.ListCheckRunsResults{}}
	opts := &github.ListOptions{PerPage: 100}
	for {
		combined, resp, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, sha, opts)
		if err != nil {
			return Result{}, fmt.Errorf("reading commit statuses: %w", err)
		}
		if combined == nil {
			return Result{}, fmt.Errorf("reading commit statuses: empty response")
		}
		result.Combined.Statuses = append(result.Combined.Statuses, combined.Statuses...)
		switch combined.GetState() {
		case "failure", "error":
			result.Failed = true
		case "pending":
			result.Pending = result.Pending || combined.GetTotalCount() > 0
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	runsOpts := &github.ListCheckRunsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	for {
		runs, resp, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, runsOpts)
		if err != nil {
			return Result{}, fmt.Errorf("reading check runs: %w", err)
		}
		if runs == nil {
			return Result{}, fmt.Errorf("reading check runs: empty response")
		}
		result.Runs.CheckRuns = append(result.Runs.CheckRuns, runs.CheckRuns...)
		for _, run := range runs.CheckRuns {
			if run.GetStatus() != "completed" {
				result.Pending = true
				continue
			}
			switch run.GetConclusion() {
			case "success", "neutral", "skipped":
			default:
				result.Failed = true
			}
		}
		if resp.NextPage == 0 {
			return result, nil
		}
		runsOpts.Page = resp.NextPage
	}
}
