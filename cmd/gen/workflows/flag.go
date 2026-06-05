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
	flagChangelogStyle                = "changelog-style"
	flagAutoReleaseLevel              = "auto-release-level"
	flagRepoName                      = "repo-name"
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
	ChangelogStyle                string
	AutoReleaseLevel              string
	RepoName                      string
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
	cmd.Flags().StringVar(&f.ReleaseWorkflow, flagReleaseWorkflow, "legacy", "Release workflow to generate. legacy: the create-release-pr/create-release/validate-changelog trio. release-please: the Release Please flow. auto-release: the push-based git-cliff flow (auto-release.yaml + cliff.toml; tags on push to main/release-* from conventional commits). Possible values: legacy (default), release-please, auto-release.")
	cmd.Flags().StringVar(&f.ChangelogStyle, flagChangelogStyle, "legacy", "Changelog section style for release-please. 'legacy' maps conventional commit types to ### Added/Changed/Fixed. 'release-please' uses the Release Please Angular preset. Possible values: legacy (default), release-please.")
	cmd.Flags().StringVar(&f.AutoReleaseLevel, flagAutoReleaseLevel, "none", "Automatically merge the release-please Release PR when CI passes, up to this bump level. Sets the reusable workflow's 'auto-merge-level' input. Only used with --release-workflow=release-please. Possible values: none (default), patch, minor, major.")
	cmd.Flags().StringVar(&f.RepoName, flagRepoName, "", "Repository name under the giantswarm organization. Required with --release-workflow=auto-release: templated into cliff.toml's [remote.github] section so git-cliff resolves PR links and authors.")
}

func (f *flag) Validate() error {
	if len(f.Flavours) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s", flagFlavour, strings.Join(gen.AllFlavours(), ", "))
	}

	switch f.ReleaseWorkflow {
	case "legacy", "release-please", "auto-release":
		// valid
	default:
		return microerror.Maskf(invalidFlagError, "--%s must be one of: legacy, release-please, auto-release", flagReleaseWorkflow)
	}

	switch f.AutoReleaseLevel {
	case "none", "patch", "minor", "major":
		// valid
	default:
		return microerror.Maskf(invalidFlagError, "--%s must be one of: none, patch, minor, major", flagAutoReleaseLevel)
	}

	if f.ReleaseWorkflow == "auto-release" && f.RepoName == "" {
		return microerror.Maskf(invalidFlagError, "--%s is required with --%s=auto-release (it is templated into cliff.toml)", flagRepoName, flagReleaseWorkflow)
	}

	return nil
}
