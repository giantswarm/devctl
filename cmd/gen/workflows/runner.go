package workflows

import (
	"context"
	"io"
	"os"

	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/gen"
	"github.com/giantswarm/devctl/v7/pkg/gen/input"
	"github.com/giantswarm/devctl/v7/pkg/gen/input/workflows"
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

	var workflowsInput *workflows.Workflows
	{
		c := workflows.Config{
			Flavours: r.flag.Flavours,
		}

		workflowsInput, err = workflows.New(c)
		if err != nil {
			return microerror.Mask(err)
		}
	}

	inputs := []input.Input{
		workflowsInput.SemanticPullRequest(),
	}

	if r.flag.ReleaseWorkflow == "release-please" {
		_, statErr := os.Stat("pkg/project/project.go")
		hasProjectGo := r.flag.Language == "go" && statErr == nil
		inputs = append(inputs,
			workflowsInput.ReleasePlease(r.flag.AutoReleaseLevel),
			workflowsInput.ReleasePleaseConfig(r.flag.ChangelogStyle, hasProjectGo),
			workflowsInput.ReleasePleaseManifest(),
			// A repo uses either the legacy create-release flow or release-please,
			// never both — the two would race over CHANGELOG.md and tags. When a
			// repo opts into release-please, remove the legacy workflow files that
			// a previous gen run may have left behind.
			workflowsInput.CreateReleaseDeletion(),
			workflowsInput.CreateReleasePRDeletion(),
			workflowsInput.ValidateChangelogDeletion(),
		)
	} else {
		inputs = append(inputs,
			workflowsInput.CreateRelease(),
			workflowsInput.CreateReleasePR(),
			workflowsInput.ValidateChangelog(),
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

	// release-please is commit-driven, so the curated "## [Unreleased]"
	// section is no longer the source of upcoming release notes. Drop it on
	// every gen run; otherwise release-please's next-version insert lands
	// above it and the "[Unreleased]" header gets stranded mid-file.
	if r.flag.ReleaseWorkflow == "release-please" {
		if err := workflows.RemoveChangelogUnreleasedSection("CHANGELOG.md"); err != nil {
			return microerror.Mask(err)
		}
	}

	return nil
}
