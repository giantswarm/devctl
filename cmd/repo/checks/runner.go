package checks

import (
	"context"
	"errors"
	"io"
	"regexp"

	"github.com/giantswarm/microerror"
	"github.com/google/go-github/v92/github"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/engine"
	"github.com/giantswarm/devctl/v8/pkg/reposetup"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/reconcile"
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

	return microerror.Mask(r.run(ctx, args[0]))
}

// run runs the protection step of the set-up engine over the required
// checks alone: the branch's reviews, admin enforcement and strict setting
// are read first and handed to the step as its baseline, so the step finds
// no drift in them and only the required contexts change.
func (r *runner) run(ctx context.Context, arg string) error {
	r.logger.SetOutput(r.stderr)

	owner, repo, err := engine.Slug(arg, "")
	if err != nil {
		return microerror.Mask(err)
	}

	client, err := engine.GitHubClient(r.logger, r.flag.GithubTokenEnvVar, false, nil)
	if err != nil {
		return microerror.Mask(err)
	}
	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}
	branch := repository.GetDefaultBranch()
	gh := client.GetUnderlyingClient(ctx)

	protection, resp, err := gh.Repositories.GetBranchProtection(ctx, owner, repo, branch)
	switch {
	case errors.Is(err, github.ErrBranchNotProtected) || (resp != nil && resp.StatusCode == 404):
		r.logger.Warnf("%s/%s: branch %q has no protection, skipping", owner, repo, branch)
		return nil
	case err != nil:
		return microerror.Mask(err)
	}

	baseline := checksBaseline(protection, r.flag.Checks, r.flag.ChecksIfReported, r.flag.Remove)

	// --circleci-dir: the pipeline as just generated, before it is pushed;
	// without it the step reads the repository's .circleci.
	var pipeline [][]byte
	if r.flag.CircleCIDir != "" {
		pipeline, err = engine.PipelineFiles(r.flag.CircleCIDir)
		switch {
		case err != nil:
			r.logger.Warnf("%s/%s: cannot read the CircleCI pipeline in %q (%v); reading the repository's .circleci instead", owner, repo, r.flag.CircleCIDir, err)
			pipeline = nil
		case len(pipeline) == 0:
			r.logger.Warnf("%s/%s: no workflows.yml or custom.yml in %q; reading the repository's .circleci instead", owner, repo, r.flag.CircleCIDir)
			pipeline = nil
		}
	}

	mode := reconcile.ModeCheck
	if r.flag.Update {
		mode = reconcile.ModeRepair
	}
	engineRunner := reconcile.Runner{
		GitHub:   gh,
		Checks:   client,
		Baseline: &baseline,
		Log:      engine.LogWriter(r.logger),
	}
	res, err := engineRunner.Run(ctx, reconcile.Request{
		Owner:    owner,
		Entry:    reposetup.UndeclaredEntry(reposetup.Undeclared{Name: repo}),
		Mode:     mode,
		Steps:    []reconcile.Step{reconcile.StepProtection},
		Pipeline: pipeline,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	return microerror.Mask(engine.Report(r.stdout, res, r.flag.Output))
}

// checksBaseline is the protection step's baseline for this command: the
// branch's current reviews, admin enforcement and strict setting (left as
// they are), --checks required whatever reported, --checks-if-reported
// required once reported, and --remove never required — on top of the
// company's ignored contexts and reported-only rule.
func checksBaseline(protection *github.Protection, checks, checksIfReported, remove []string) reconcile.Baseline {
	b := reconcile.DefaultBaseline()
	b.RequiredReviews = protection.GetRequiredPullRequestReviews().GetRequiredApprovingReviewCount()
	b.EnforceAdmins = protection.GetEnforceAdmins().GetEnabled()
	if protection.GetRequiredStatusChecks() != nil {
		b.StrictChecks = protection.GetRequiredStatusChecks().Strict
	}
	b.RequiredChecks = checks
	b.RequiredChecksIfReported = checksIfReported
	for _, name := range remove {
		b.IgnoredChecks = append(b.IgnoredChecks, "^"+regexp.QuoteMeta(name)+"$")
	}
	return b
}
