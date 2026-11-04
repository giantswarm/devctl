package approvealign

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// combinedStatuses keeps the aggregate state and every context used by the
// missing-required-check diagnostic. The aggregate state covers all pages.
func combinedStatuses(ctx context.Context, client *github.Client, owner, repo, sha string) (*github.CombinedStatus, error) {
	opts := &github.ListOptions{PerPage: 100}
	var combined *github.CombinedStatus
	for {
		page, resp, err := client.Repositories.GetCombinedStatus(ctx, owner, repo, sha, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if page == nil {
			return nil, microerror.Maskf(executionFailedError, "empty combined-status response")
		}
		if combined == nil {
			combined = page
		} else {
			combined.Statuses = append(combined.Statuses, page.Statuses...)
		}
		if resp.NextPage == 0 {
			return combined, nil
		}
		opts.Page = resp.NextPage
	}
}

// allCheckRuns reads the latest runs across every page before the runner can
// decide the head is green. An error discards the partial result.
func allCheckRuns(ctx context.Context, client *github.Client, owner, repo, sha string) (*github.ListCheckRunsResults, error) {
	opts := &github.ListCheckRunsOptions{ListOptions: github.ListOptions{PerPage: 100}}
	combined := &github.ListCheckRunsResults{}
	for {
		page, resp, err := client.Checks.ListCheckRunsForRef(ctx, owner, repo, sha, opts)
		if err != nil {
			return nil, microerror.Mask(err)
		}
		if page == nil {
			return nil, microerror.Maskf(executionFailedError, "empty check-runs response")
		}
		combined.Total = page.Total
		combined.CheckRuns = append(combined.CheckRuns, page.CheckRuns...)
		if resp.NextPage == 0 {
			return combined, nil
		}
		opts.Page = resp.NextPage
	}
}
