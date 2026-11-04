package checks

import (
	"context"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v93/github"
)

// classicChecksWriter changes the checks subresource alone. Rebuilding the
// whole protection from the setup baseline would reset review controls, push
// restrictions and other settings this command does not own.
type classicChecksWriter struct{ github *github.Client }

func (w classicChecksWriter) Write(ctx context.Context, owner, repo, branch string, protection *github.Protection, want []string) error {
	if protection == nil {
		return microerror.Mask(github.ErrBranchNotProtected)
	}
	if len(want) == 0 {
		_, err := w.github.Repositories.RemoveRequiredStatusChecks(ctx, owner, repo, branch)
		return microerror.Mask(err)
	}
	current := protection.GetRequiredStatusChecks()
	pins := map[string]*int64{}
	if current != nil && current.Checks != nil {
		for _, check := range *current.Checks {
			pins[check.Context] = check.AppID
		}
	}
	checks := make([]*github.RequiredStatusCheck, 0, len(want))
	for _, name := range want {
		checks = append(checks, &github.RequiredStatusCheck{Context: name, AppID: pins[name]})
	}
	_, _, err := w.github.Repositories.UpdateRequiredStatusChecks(ctx, owner, repo, branch, &github.RequiredStatusChecksRequest{
		Strict: new(current.GetStrict()),
		Checks: checks,
	})
	return microerror.Mask(err)
}
