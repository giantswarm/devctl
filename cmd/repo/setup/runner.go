package setup

import (
	"context"
	"io"
	"strings"

	"github.com/giantswarm/microerror"
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

	client, err := engine.GitHubClient(r.logger, r.flag.GithubTokenEnvVar, r.flag.DryRun)
	if err != nil {
		return microerror.Mask(err)
	}

	repository, err := client.GetRepository(ctx, owner, repo)
	if err != nil {
		return microerror.Mask(err)
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

	return microerror.Mask(engine.Report(r.stdout, res, r.flag.Output))
}

// baseline is the company baseline with the flags applied: the features,
// merge and pull-request settings, the team permissions (team names as
// slugs), the default branch, --checks as the unconditional contexts and
// --checks-filter as one more ignored pattern.
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
	if f.ChecksFilter != "" {
		b.IgnoredChecks = append(b.IgnoredChecks, f.ChecksFilter)
	}
	return b
}
