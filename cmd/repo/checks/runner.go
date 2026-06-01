package checks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v88/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/githubclient"
)

type runner struct {
	flag   *flag
	logger *logrus.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if err := r.flag.Validate(); err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(r.run(ctx, cmd, args))
}

func (r *runner) run(ctx context.Context, _ *cobra.Command, args []string) error {
	parts := strings.SplitN(args[0], "/", 2)
	if len(parts) != 2 {
		return microerror.Maskf(invalidArgError, "expected owner/repo, got %s", args[0])
	}

	owner, repo := parts[0], parts[1]

	if r.flag.Update {
		return microerror.Mask(r.update(ctx, owner, repo))
	}

	return nil
}

func (r *runner) update(ctx context.Context, owner, repo string) error {
	token, found := os.LookupEnv(r.flag.GithubTokenEnvVar)
	if !found {
		return microerror.Maskf(envVarNotFoundError, "environment variable %#q was not found", r.flag.GithubTokenEnvVar)
	}

	client, err := githubclient.New(githubclient.Config{
		Logger:      r.logger,
		AccessToken: token,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}

	defaultBranch := repository.GetDefaultBranch()
	underlying := client.GetUnderlyingClient(ctx)

	current, _, err := underlying.Repositories.GetRequiredStatusChecks(ctx, owner, repo, defaultBranch)
	if err != nil {
		if errors.Is(err, github.ErrBranchNotProtected) {
			r.logger.Warnf("%s/%s: branch %q has no protection, skipping", owner, repo, defaultBranch)
			return nil
		}
		var ghErr *github.ErrorResponse
		if !errors.As(err, &ghErr) || ghErr.Response.StatusCode != http.StatusNotFound {
			return microerror.Mask(err)
		}
		// Branch is protected but required status checks not yet configured.
		// PATCH won't work in this state; fall back to a full UpdateBranchProtection.
		return microerror.Mask(r.enableViaFullProtection(ctx, underlying, owner, repo, defaultBranch))
	}

	merged := applyChecks(current.GetChecks(), r.flag.Checks, r.flag.Remove)

	strict := current.Strict
	_, _, err = underlying.Repositories.UpdateRequiredStatusChecks(ctx, owner, repo, defaultBranch, &github.RequiredStatusChecksRequest{
		Strict: &strict,
		Checks: merged,
	})

	r.logger.Infof("%s/%s: required checks on %q: added %v, removed %v", owner, repo, defaultBranch, r.flag.Checks, r.flag.Remove)

	return microerror.Mask(err)
}

// enableViaFullProtection reads the current branch protection and issues a full
// UpdateBranchProtection that enables required status checks while preserving
// all other existing protection settings.
func (r *runner) enableViaFullProtection(ctx context.Context, underlying *github.Client, owner, repo, branch string) error {
	protection, _, err := underlying.Repositories.GetBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		return microerror.Mask(err)
	}

	merged := applyChecks(nil, r.flag.Checks, nil)
	False := false

	req := &github.ProtectionRequest{
		RequiredStatusChecks: &github.RequiredStatusChecks{
			Strict: false,
			Checks: &merged,
		},
		AllowForcePushes: &False,
		AllowDeletions:   &False,
	}

	if ea := protection.GetEnforceAdmins(); ea != nil {
		req.EnforceAdmins = ea.Enabled
	}
	if afp := protection.GetAllowForcePushes(); afp != nil {
		req.AllowForcePushes = &afp.Enabled
	}
	if ad := protection.GetAllowDeletions(); ad != nil {
		req.AllowDeletions = &ad.Enabled
	}
	if rpr := protection.GetRequiredPullRequestReviews(); rpr != nil {
		req.RequiredPullRequestReviews = &github.PullRequestReviewsEnforcementRequest{
			RequiredApprovingReviewCount: rpr.RequiredApprovingReviewCount,
			DismissStaleReviews:          rpr.DismissStaleReviews,
			RequireCodeOwnerReviews:      rpr.RequireCodeOwnerReviews,
		}
	}

	r.logger.Infof("%s/%s: enabling required checks %v on %q via full branch protection update", owner, repo, r.flag.Checks, branch)

	_, _, err = underlying.Repositories.UpdateBranchProtection(ctx, owner, repo, branch, req)
	return microerror.Mask(err)
}

func applyChecks(existing []*github.RequiredStatusCheck, add, remove []string) []*github.RequiredStatusCheck {
	drop := make(map[string]bool, len(remove))
	for _, name := range remove {
		drop[name] = true
	}

	seen := make(map[string]bool, len(existing))
	merged := make([]*github.RequiredStatusCheck, 0, len(existing)+len(add))
	for _, c := range existing {
		if drop[c.GetContext()] {
			continue
		}
		merged = append(merged, c)
		seen[c.GetContext()] = true
	}
	for _, name := range add {
		if !seen[name] {
			merged = append(merged, &github.RequiredStatusCheck{Context: name})
			seen[name] = true
		}
	}
	return merged
}
