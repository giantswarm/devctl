package setup

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/engine"
	"github.com/giantswarm/devctl/v8/pkg/githubclient"
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

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, args[0])
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// run applies the flags as the baseline of the set-up engine's settings,
// permissions, protection and Renovate steps (lifecycle with --archived).
// The default-branch rename and --disable-branch-protection have no step
// and stay direct calls.
func (r *runner) run(ctx context.Context, arg string) error {
	r.logger.SetOutput(r.stderr)

	owner, repo, err := engine.Slug(arg, "")
	if err != nil {
		return microerror.Mask(err)
	}

	client, err := engine.GitHubClient(r.logger, r.flag.GithubTokenEnvVar, r.flag.DryRun, nil)
	if err != nil {
		return microerror.Mask(err)
	}

	return r.runWithClient(ctx, owner, repo, client)
}

// runWithClient applies setup with an already constructed client.
func (r *runner) runWithClient(ctx context.Context, owner, repo string, client *githubclient.Client) error {
	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
	}

	if r.flag.DisableBranchProtection {
		if err := checkRulesetDisable(ctx, client, owner, repo, repository.GetDefaultBranch(), r.flag.DefaultBranch); err != nil {
			return microerror.Mask(err)
		}
	}

	err = client.SetRepositoryDefaultBranch(ctx, repository, r.flag.DefaultBranch)
	if err != nil {
		return microerror.Mask(err)
	}

	steps := []reconcile.Step{reconcile.StepSettings, reconcile.StepPermissions}
	if r.flag.DisableBranchProtection {
		err = client.RemoveRepositoryBranchProtection(ctx, repository)
		if err != nil {
			return microerror.Mask(err)
		}
	} else {
		steps = append(steps, reconcile.StepProtection)
	}
	if r.flag.SetupRenovate {
		steps = append(steps, reconcile.StepRenovate)
	}

	entry := reposetup.Undeclared{Name: repo}
	if r.flag.Archived {
		entry.Lifecycle = reconcile.LifecycleArchived
		steps = append(steps, reconcile.StepLifecycle)
	}

	mode := reconcile.ModeRepair
	if r.flag.DryRun {
		mode = reconcile.ModeCheck
	}
	baseline := r.baseline()
	engineRunner := reconcile.Runner{
		GitHub:   client.GetUnderlyingClient(ctx),
		Checks:   client,
		Baseline: &baseline,
		Log:      engine.LogWriter(r.logger),
	}
	res, err := engineRunner.Run(ctx, reconcile.Request{
		Owner: owner,
		Entry: reposetup.UndeclaredEntry(entry),
		Mode:  mode,
		Steps: steps,
	})
	if err != nil {
		return microerror.Mask(err)
	}

	return r.report(res)
}

// baseline is the company baseline with the flags applied: the features,
// merge and pull-request settings, the team permissions (team names as
// slugs), the default branch, --checks as the complete set of unconditional
// contexts when supplied, and --checks-filter as one more ignored pattern
// when checks are detected automatically.
func (r *runner) baseline() reconcile.Baseline {
	f := r.flag
	b := reconcile.DefaultBaseline()
	b.DefaultBranch = f.DefaultBranch
	b.HasWiki = f.EnableWiki
	b.HasIssues = f.EnableIssues
	b.HasProjects = f.EnableProjects
	b.AllowMergeCommit = f.AllowMergeCommit
	b.AllowSquashMerge = f.AllowSquashMerge
	b.AllowRebaseMerge = f.AllowRebaseMerge
	b.AllowUpdateBranch = f.AllowUpdateBranch
	b.AllowAutoMerge = f.AllowAutoMerge
	b.DeleteBranchOnMerge = f.DeleteBranchOnMerge
	b.TeamPermissions = make(map[string]string, len(f.Permissions))
	for team, permission := range f.Permissions {
		b.TeamPermissions[strings.ToLower(team)] = permission
	}
	b.RequiredChecks = f.Checks
	if f.Checks != nil {
		// An explicit list replaces both the current and detected contexts.
		// The non-nil empty slice from --checks= therefore requires none.
		b.RequiredChecksIfReported = nil
		b.IgnoredChecks = []string{".*"}
	} else if f.ChecksFilter != "" {
		b.IgnoredChecks = append(b.IgnoredChecks, f.ChecksFilter)
	}
	return b
}

// report gives setup a usable refusal when the shared engine cannot apply a
// ruleset repair. Check mode still reports drift without failing.
func (r *runner) report(res *reconcile.Result) error {
	pending := false
	for i := range res.Steps {
		for j := range res.Steps[i].Findings {
			finding := &res.Steps[i].Findings[j]
			if finding.Kind == reconcile.FindingRulesetPending {
				pending = true
				finding.Fix = "repo setup cannot write rulesets; use the repository reconciler's Align now action with its configured App IDs, or devctl repo reconcile --team-file=<team-file> --steps=protection --devctl-app-id=<numeric-App-id> " + res.Repository + " with the same configured align-files and release App IDs"
			}
		}
	}
	if err := engine.Report(r.stdout, res, r.flag.Output); err != nil {
		return microerror.Mask(err)
	}
	if pending && res.Mode == reconcile.ModeRepair {
		return fmt.Errorf("repo setup left ruleset protection pending; use the repository reconciler's Align now action or devctl repo reconcile as described in the result")
	}
	return nil
}
