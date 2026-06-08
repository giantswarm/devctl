package workflows

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	flagCheckSecrets                  = "check-secrets"
	flagFlavour                       = "flavour"
	flagLanguage                      = "language"
	flagInstallUpdateChart            = "install-update-chart"
	flagRunSecurityScorecard          = "run-security-scorecard"
	flagAnalyzeGithubActions          = "analyze-github-actions"
	flagPublishTechdocs               = "publish-techdocs"
	flagUpstreamSyncAutomation        = "upstream-sync-automation"
	flagDispatchUpdateChartEventsRepo = "dispatch-update-chart-events-repo"
	flagReleaseWorkflow               = "release-workflow"

	releaseWorkflowLegacy      = "legacy"
	releaseWorkflowAutoRelease = "auto-release"
)

type flag struct {
	CheckSecrets                  bool
	Flavours                      gen.FlavourSlice
	Language                      string
	InstallUpdateChart            bool
	RunSecurityScorecard          bool
	AnalyzeGithubActions          bool
	PublishTechdocs               bool
	UpstreamSyncAutomation        bool
	DispatchUpdateChartEventsRepo string
	ReleaseWorkflow               string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.CheckSecrets, flagCheckSecrets, true, "If true, also generate a secret-scanning workflow. Possible values: true (default), false.")
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate the workflows for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language of the repo, for generating additional language-specific workflows, like vulnerability remediation.")
	cmd.Flags().BoolVar(&f.InstallUpdateChart, flagInstallUpdateChart, false, "If true, also generate update_chart workflow. Only valid for app flavor.")
	cmd.Flags().BoolVar(&f.RunSecurityScorecard, flagRunSecurityScorecard, true, "If true, also generate a security scorecard workflow. Possible values: true (default), false.")
	cmd.Flags().BoolVar(&f.AnalyzeGithubActions, flagAnalyzeGithubActions, false, "If true, also generate a workflow for GitHub Actions security scanning. Possible values: false (default), true.")
	cmd.Flags().BoolVar(&f.PublishTechdocs, flagPublishTechdocs, false, "If true, also generate the Publish Techdocs workflow. Possible values: false (default), true.")
	cmd.Flags().BoolVar(&f.UpstreamSyncAutomation, flagUpstreamSyncAutomation, false, "If true, also generate a workflow to dispatch update events for charts. Only valid for app flavor.")
	cmd.Flags().StringVar(&f.DispatchUpdateChartEventsRepo, flagDispatchUpdateChartEventsRepo, "", "The repository to dispatch update chart events to. Only valid if --upstream-sync-automation is true.")
	cmd.Flags().StringVar(&f.ReleaseWorkflow, flagReleaseWorkflow, releaseWorkflowLegacy, fmt.Sprintf("Release workflow to generate. Possible values: %s (default), %s. %s generates the create-release-pr / create-release / validate-changelog trio; %s generates a single push-based auto-release.yaml + cliff.toml that tags + publishes a GitHub Release from conventional commits.", releaseWorkflowLegacy, releaseWorkflowAutoRelease, releaseWorkflowLegacy, releaseWorkflowAutoRelease))
}

func (f *flag) Validate() error {
	if len(f.Flavours) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s", flagFlavour, strings.Join(gen.AllFlavours(), ", "))
	}

	switch f.ReleaseWorkflow {
	case releaseWorkflowLegacy, releaseWorkflowAutoRelease:
		// valid
	default:
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s, %s", flagReleaseWorkflow, releaseWorkflowLegacy, releaseWorkflowAutoRelease)
	}

	return nil
}
