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
	flagHelmDocsRegen                 = "helm-docs-regen"
	flagRunSecurityScorecard          = "run-security-scorecard"
	flagAnalyzeGithubActions          = "analyze-github-actions"
	flagPublishTechdocs               = "publish-techdocs"
	flagUpstreamSyncAutomation        = "upstream-sync-automation"
	flagDispatchUpdateChartEventsRepo = "dispatch-update-chart-events-repo"
	flagReleaseBranch                 = "release-branch"
	flagMaintenanceBranches           = "maintenance-branches"
	flagReleaseWorkflow               = "release-workflow"
	flagRepoName                      = "repo-name"

	releaseWorkflowLegacy      = "legacy"
	releaseWorkflowAutoRelease = "auto-release"
)

type flag struct {
	CheckSecrets                  bool
	Flavours                      gen.FlavourSlice
	Language                      string
	InstallUpdateChart            bool
	HelmDocsRegen                 bool
	RunSecurityScorecard          bool
	AnalyzeGithubActions          bool
	PublishTechdocs               bool
	UpstreamSyncAutomation        bool
	DispatchUpdateChartEventsRepo string
	ReleaseBranch                 string
	MaintenanceBranches           bool
	ReleaseWorkflow               string
	RepoName                      string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.CheckSecrets, flagCheckSecrets, true, "If true, also generate a secret-scanning workflow. Possible values: true (default), false.")
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate the workflows for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language of the repo, for generating additional language-specific workflows, like vulnerability remediation.")
	cmd.Flags().BoolVar(&f.InstallUpdateChart, flagInstallUpdateChart, false, "If true, also generate update_chart workflow. Only valid for app flavor.")
	cmd.Flags().BoolVar(&f.HelmDocsRegen, flagHelmDocsRegen, false, "If true, also generate the helm-docs-regen workflow, which regenerates the chart README (helm-docs) and values.schema.json (helm-schema-<chart> hooks) on renovate/ and dependabot/ PR branches and pushes the result back onto the branch. Only valid for app flavor.")
	cmd.Flags().BoolVar(&f.RunSecurityScorecard, flagRunSecurityScorecard, true, "If true, also generate a security scorecard workflow. Possible values: true (default), false.")
	cmd.Flags().BoolVar(&f.AnalyzeGithubActions, flagAnalyzeGithubActions, false, "If true, also generate a workflow for GitHub Actions security scanning. Possible values: false (default), true.")
	cmd.Flags().BoolVar(&f.PublishTechdocs, flagPublishTechdocs, false, "If true, also generate the Publish Techdocs workflow. Possible values: false (default), true.")
	cmd.Flags().BoolVar(&f.UpstreamSyncAutomation, flagUpstreamSyncAutomation, false, "If true, also generate a workflow to dispatch update events for charts. Only valid for app flavor.")
	cmd.Flags().StringVar(&f.DispatchUpdateChartEventsRepo, flagDispatchUpdateChartEventsRepo, "", "The repository to dispatch update chart events to. Only valid if --upstream-sync-automation is true.")
	cmd.Flags().StringVar(&f.ReleaseWorkflow, flagReleaseWorkflow, releaseWorkflowLegacy, fmt.Sprintf("Release workflow to generate. Possible values: %s (default), %s. %s generates the create-release-pr / create-release / validate-changelog trio; %s generates a single push-based zz_generated.auto_release.yaml + cliff.toml that tags + publishes a GitHub Release from conventional commits.", releaseWorkflowLegacy, releaseWorkflowAutoRelease, releaseWorkflowLegacy, releaseWorkflowAutoRelease))
	cmd.Flags().StringVar(&f.ReleaseBranch, flagReleaseBranch, "main", fmt.Sprintf("Branch whose pushes cut releases, needed only with --%s=%s: the repository's default branch, `giantswarm` on a fork line.", flagReleaseWorkflow, releaseWorkflowAutoRelease))
	cmd.Flags().BoolVar(&f.MaintenanceBranches, flagMaintenanceBranches, false, fmt.Sprintf("If true, a fork line's maintenance branches release-X.Y cut releases too, each the patches of its own X.Y series counted from the series' highest stable tag. Only valid with --%s=%s and --%s=%s.", flagFlavour, gen.FlavourFork, flagReleaseWorkflow, releaseWorkflowAutoRelease))
	cmd.Flags().StringVar(&f.RepoName, flagRepoName, "", fmt.Sprintf("Repository name under the giantswarm organization for cliff.toml's [remote.github].repo field, needed only with --%s=%s. Defaults to <name> in the giantswarm/<name> path of the git origin remote (https or ssh URL, .git stripped); without an origin remote, or with one outside the giantswarm organization (a scaffold rendered into a bare directory has neither), the command fails and asks for this flag. The directory name is never used.", flagReleaseWorkflow, releaseWorkflowAutoRelease))
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

	if f.MaintenanceBranches && (!f.Flavours.Contains(gen.FlavourFork) || f.ReleaseWorkflow != releaseWorkflowAutoRelease) {
		return microerror.Maskf(invalidFlagError, "--%s is only valid with --%s=%s and --%s=%s: a repository of another flavour releases its release-X.x backport branches already", flagMaintenanceBranches, flagFlavour, gen.FlavourFork, flagReleaseWorkflow, releaseWorkflowAutoRelease)
	}

	return nil
}
