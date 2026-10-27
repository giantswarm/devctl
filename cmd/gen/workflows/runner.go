package workflows

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/gitremote"
	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/workflows"
)

type runner struct {
	flag   *flag
	logger micrologger.Logger
	stdout io.Writer
	stderr io.Writer
}

func (r *runner) Run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	err := r.flag.Validate()
	if err != nil {
		return microerror.Mask(err)
	}

	err = r.run(ctx, cmd, args)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func (r *runner) run(ctx context.Context, _ *cobra.Command, _ []string) error {
	var err error

	// A fork line carries its upstream's files: nothing is generated for it
	// but its release flow when it is on auto-release.
	fork := !r.flag.Flavours.Generates()
	if fork && r.flag.ReleaseWorkflow != releaseWorkflowAutoRelease {
		return nil
	}

	// cliff.toml (auto-release only) carries its own repository's name in
	// [remote.github].repo. Unlike the rest of what's generated, that name
	// isn't in the declaration this command reads, so it's read from the
	// origin remote, with --repo-name as the override a render with no real
	// checkout (e.g. devctl repo create's scaffold) has to pass explicitly.
	// A multi-image repository releases without git-cliff, so it has no
	// cliff.toml to name the repository in.
	multiImage := r.flag.Flavours.Contains(gen.FlavourMultiImage)
	repoName := r.flag.RepoName
	if repoName == "" && r.flag.ReleaseWorkflow == releaseWorkflowAutoRelease && !multiImage {
		repoName, err = gitremote.RepoName(ctx, ".")
		if err != nil {
			return microerror.Maskf(invalidFlagError, "cliff.toml's [remote.github].repo cannot be read from the origin remote: %s; pass --%s <name>", err, flagRepoName)
		}
	}

	var workflowsInput *workflows.Workflows
	{
		c := workflows.Config{
			Flavours:            r.flag.Flavours,
			ReleaseBranch:       r.flag.ReleaseBranch,
			MaintenanceBranches: r.flag.MaintenanceBranches,
			PatchCandidates:     r.flag.PatchCandidates,
			RepoName:            repoName,
		}

		workflowsInput, err = workflows.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	inputs := []input.Input{
		workflowsInput.SemanticPullRequest(),
	}

	// A fork line on auto-release gets the release flow alone: the title
	// check auto-release relies on, the workflow and its cliff.toml. The
	// legacy trio is not deleted there; a fork line never had it generated.
	if fork {
		inputs = append(inputs,
			workflowsInput.AutoRelease(),
			workflowsInput.CliffToml(),
		)
		return microerror.Mask(gen.Execute(ctx, inputs...))
	}

	// Two mutually-exclusive release flows. Each branch emits the workflow
	// files it owns AND deletion inputs for the OTHER flow's files, so
	// flipping `--release-workflow` (or the matching `releaseWorkflow:` in
	// giantswarm/github) in either direction leaves the repo with exactly
	// one set of release files. The un-prefixed `auto-release.yaml` left
	// over from manual migrations predating the `zz_generated.` rename is
	// deleted in BOTH branches via AutoReleaseLegacyDeletion, and so is the
	// retired zz_generated.changelog.yaml via ChangelogDeletion.
	if multiImage {
		// Validate admits a multi-image repository on auto-release alone. Its
		// workflow takes the auto-release path, so a switch from either flow
		// to it, or back, leaves one set of release files.
		inputs = append(inputs,
			workflowsInput.AutoReleaseLegacyDeletion(),
			workflowsInput.AutoReleaseMultiImage(),
			workflowsInput.ChangelogDeletion(),
			workflowsInput.CliffTomlDeletion(),
			workflowsInput.CreateReleaseDeletion(),
			workflowsInput.CreateReleasePRDeletion(),
			workflowsInput.ValidateChangelogDeletion(),
		)
	} else if r.flag.ReleaseWorkflow == releaseWorkflowAutoRelease {
		inputs = append(inputs,
			workflowsInput.AutoReleaseLegacyDeletion(),
			workflowsInput.AutoRelease(),
			workflowsInput.ChangelogDeletion(),
			workflowsInput.CliffToml(),
			workflowsInput.CreateReleaseDeletion(),
			workflowsInput.CreateReleasePRDeletion(),
			workflowsInput.ValidateChangelogDeletion(),
		)
	} else {
		r.warnAutoReleaseDeclaredLegacy(workflowsInput.AutoReleaseDeletion(), workflowsInput.CliffTomlDeletion())
		inputs = append(inputs,
			workflowsInput.CreateRelease(),
			workflowsInput.CreateReleasePR(),
			workflowsInput.ValidateChangelog(),
			workflowsInput.AutoReleaseLegacyDeletion(),
			workflowsInput.AutoReleaseDeletion(),
			workflowsInput.ChangelogDeletion(),
			workflowsInput.CliffTomlDeletion(),
		)
	}

	if r.flag.Language == "go" {
		inputs = append(inputs, workflowsInput.FixVulnerabilities())
	}

	if r.flag.CheckSecrets {
		inputs = append(inputs, workflowsInput.Gitleaks())
	}

	if r.flag.Flavours.Contains(gen.FlavourApp) {
		inputs = append(inputs, workflowsInput.CheckValuesSchema())
		inputs = append(inputs, workflowsInput.ChartTests())
		if r.flag.CheckReadmeLinks {
			inputs = append(inputs, workflowsInput.CheckReadmeLinks())
		}
		if r.flag.HelmDocsRegen {
			inputs = append(inputs, workflowsInput.HelmDocsRegen())
		}
		if r.flag.ReservationCommand {
			inputs = append(inputs, workflowsInput.ReservationCommand())
		}
		if r.flag.InstallUpdateChart {
			inputs = append(inputs, workflowsInput.UpdateChart())
			if r.flag.UpstreamSyncAutomation {
				inputs = append(inputs, workflowsInput.SyncFromUpstream())
			}
			if r.flag.DispatchUpdateChartEventsRepo != "" {
				inputs = append(inputs, workflowsInput.DispatchUpdateChartEvents(r.flag.DispatchUpdateChartEventsRepo))
			}
		}
		if r.flag.Language == "kyverno-policy" {
			inputs = append(inputs, workflowsInput.TestKyvernoPoliciesWithChainsaw())
		}
	}

	if r.flag.Flavours.Contains(gen.FlavourCustomer) {
		inputs = append(inputs, workflowsInput.AddCustomerBoardAutomation())
	}

	if r.flag.Flavours.Contains(gen.FlavourClusterApp) {
		inputs = append(inputs, workflowsInput.ClusterAppDocumentationValidation())
		inputs = append(inputs, workflowsInput.ClusterAppSchemaValidation())
		inputs = append(inputs, workflowsInput.HelmRenderDiff())
	}

	if r.flag.RunSecurityScorecard {
		inputs = append(inputs, workflowsInput.RunOSSFScorecard())
	}

	if r.flag.AnalyzeGithubActions {
		inputs = append(inputs, workflowsInput.AnalyzeGithubActions())
		inputs = append(inputs, workflowsInput.ZizmorBaseYml())
	}

	if r.flag.Flavours.Contains(gen.FlavourManagementClustersFleet) {
		inputs = append(inputs, workflowsInput.ClusterAppValuesValidationUsingSchema())
		inputs = append(inputs, workflowsInput.Mctl()...)
		inputs = append(inputs, workflowsInput.MctlLegacyDeletion()...)
	}

	if r.flag.PublishTechdocs {
		inputs = append(inputs, workflowsInput.PublishTechdocsInput())
	}

	err = gen.Execute(
		ctx,
		inputs...,
	)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// warnAutoReleaseDeclaredLegacy names the declaration when a legacy run is
// about to delete Auto Release files the repository carries: they were added
// without the declaration saying auto-release, and every run deletes them
// again until it does.
func (r *runner) warnAutoReleaseDeclaredLegacy(deletions ...input.Input) {
	var found []string
	for _, d := range deletions {
		if _, err := os.Stat(d.Path); err == nil {
			found = append(found, d.Path)
		}
	}
	if len(found) == 0 || r.stderr == nil {
		return
	}

	fmt.Fprintf(r.stderr, "Warning: deleting %s: --%s is %s, as the repository's declaration in giantswarm/github says. Auto Release is enabled there, with gen.ci.releaseWorkflow: %s in the repository's entry; files added without it are deleted on every run.\n", strings.Join(found, " and "), flagReleaseWorkflow, releaseWorkflowLegacy, releaseWorkflowAutoRelease)
}
