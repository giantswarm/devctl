package approvealign

import (
	"context"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

func allReviews(ctx context.Context, client *github.Client, owner, repo string, number int) ([]*github.PullRequestReview, *github.Response, error) {
	opts := &github.ListOptions{PerPage: 100}
	var reviews []*github.PullRequestReview
	for {
		page, resp, err := client.PullRequests.ListReviews(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, resp, microerror.Mask(err)
		}
		reviews = append(reviews, page...)
		if resp.NextPage == 0 {
			return reviews, resp, nil
		}
		opts.Page = resp.NextPage
	}
}

// hasCurrentApproval uses the latest verdict per reviewer. GitHub lists reviews
// chronologically; comments and pending drafts do not replace a verdict.
func hasCurrentApproval(reviews []*github.PullRequestReview) bool {
	verdicts := map[string]string{}
	for _, review := range reviews {
		login := strings.ToLower(review.GetUser().GetLogin())
		if login == "" {
			continue
		}
		switch review.GetState() {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
			verdicts[login] = review.GetState()
		}
	}
	for _, state := range verdicts {
		if state == "APPROVED" {
			return true
		}
	}
	return false
}
