package checks

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v90/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/githubclient"
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

	// --circleci-dir: the pipeline's branch-side jobs become reported-only
	// candidates, and the required "ci/circleci:" contexts they no longer
	// explain are removed below (once the current checks are known). A
	// pipeline that cannot be read is left alone: nothing is removed on a
	// guess.
	ifReported := append([]string{}, r.flag.ChecksIfReported...)
	var liveCircleCI []string
	reconcileCircleCI := false
	if r.flag.CircleCIDir != "" {
		contexts, found, err := circleCIGateContexts(r.flag.CircleCIDir)
		switch {
		case err != nil:
			r.logger.Warnf("%s/%s: cannot read the CircleCI pipeline in %q (%v); its jobs are neither required nor removed in this run", owner, repo, r.flag.CircleCIDir, err)
		case !found:
			r.logger.Warnf("%s/%s: no workflows.yml or custom.yml in %q; CircleCI contexts are left as they are", owner, repo, r.flag.CircleCIDir)
		default:
			liveCircleCI = contexts
			reconcileCircleCI = true
			ifReported = append(ifReported, contexts...)
			r.logger.Infof("%s/%s: CircleCI jobs gating pull requests: %s", owner, repo, describeContexts(contexts))
		}
	}

	// The names to add: --checks unconditionally, --checks-if-reported (and
	// the pipeline's jobs) only when the context has already reported on the
	// default branch or a recently merged PR. Resolved lazily so an
	// unprotected branch costs no discovery calls.
	resolveAdd := func() ([]string, error) {
		add := append([]string{}, r.flag.Checks...)
		if len(ifReported) == 0 {
			return add, nil
		}
		reported, err := client.ReportedChecks(ctx, repository, defaultBranch)
		if err != nil {
			// Discovery reads commit statuses and check runs, which a GitHub App
			// token can only do on a private repository when the App holds the
			// "Commit statuses" / "Checks" read permissions (403 "Resource not
			// accessible by integration" otherwise). The conditional checks are
			// best-effort by definition: leave them for a later run and keep the
			// unconditional --checks update going instead of failing the caller
			// (align-files aborts the whole repository alignment on a non-zero exit).
			r.logger.Warnf("%s/%s: cannot read the reported checks on %q (%v); not requiring %v in this run", owner, repo, defaultBranch, err, ifReported)
			return add, nil
		}
		found, skipped := splitReported(ifReported, reported)
		if len(skipped) > 0 {
			r.logger.Infof("%s/%s: not requiring %v yet: not reported on %q or a recently merged pull request", owner, repo, skipped, defaultBranch)
		}
		return append(add, found...), nil
	}

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
		add, err := resolveAdd()
		if err != nil {
			return microerror.Mask(err)
		}
		return microerror.Mask(r.enableViaFullProtection(ctx, underlying, owner, repo, defaultBranch, add))
	}

	add, err := resolveAdd()
	if err != nil {
		return microerror.Mask(err)
	}

	remove := append([]string{}, r.flag.Remove...)
	if reconcileCircleCI {
		stale := staleCircleCIContexts(current.GetChecks(), liveCircleCI)
		if len(stale) > 0 {
			r.logger.Infof("%s/%s: removing required CircleCI contexts without a job in the pipeline: %v", owner, repo, stale)
			remove = append(remove, stale...)
		}
	}

	merged := applyChecks(current.GetChecks(), add, remove)

	// UpdateRequiredStatusChecks uses omitempty, so an empty Checks slice is
	// dropped from the request and GitHub leaves the existing checks unchanged.
	// Use the DELETE endpoint when the result is empty.
	if len(merged) == 0 {
		_, err = underlying.Repositories.RemoveRequiredStatusChecks(ctx, owner, repo, defaultBranch)
		r.logger.Infof("%s/%s: removed all required checks on %q", owner, repo, defaultBranch)
		return microerror.Mask(err)
	}

	strict := current.Strict
	_, _, err = underlying.Repositories.UpdateRequiredStatusChecks(ctx, owner, repo, defaultBranch, &github.RequiredStatusChecksRequest{
		Strict: &strict,
		Checks: merged,
	})

	r.logger.Infof("%s/%s: required checks on %q: added %v, removed %v", owner, repo, defaultBranch, add, remove)

	return microerror.Mask(err)
}

// enableViaFullProtection reads the current branch protection and issues a full
// UpdateBranchProtection that enables required status checks while preserving
// all other existing protection settings.
func (r *runner) enableViaFullProtection(ctx context.Context, underlying *github.Client, owner, repo, branch string, add []string) error {
	protection, _, err := underlying.Repositories.GetBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		return microerror.Mask(err)
	}

	merged := applyChecks(nil, add, nil)
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

	r.logger.Infof("%s/%s: enabling required checks %v on %q via full branch protection update", owner, repo, add, branch)

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

// splitReported partitions candidates into the names that appear in reported
// (found) and the rest (skipped), in candidate order and without duplicates.
func splitReported(candidates, reported []string) (found, skipped []string) {
	seen := make(map[string]bool, len(reported))
	for _, name := range reported {
		seen[name] = true
	}
	done := make(map[string]bool, len(candidates))
	for _, name := range candidates {
		if done[name] {
			continue
		}
		done[name] = true
		if seen[name] {
			found = append(found, name)
		} else {
			skipped = append(skipped, name)
		}
	}
	return found, skipped
}
