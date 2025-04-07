package bootstrap

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagName          = "name"
	flagUpstreamRepo  = "upstream-repo"
	flagUpstreamChart = "upstream-chart"
	flagTeam          = "team"
	flagSyncMethod    = "sync-method"
	flagPatchMethod   = "patch-method"
	flagGithubToken   = "github-token-envvar" // #nosec G101
	flagDryRun        = "dry-run"

	// Method types
	methodKustomize = "kustomize"
	methodVendir    = "vendir"
	methodScript    = "script"
)

type flag struct {
	Name          string
	UpstreamRepo  string
	UpstreamChart string
	Team          string
	SyncMethod    string
	PatchMethod   string
	GithubToken   string
	DryRun        bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Name, flagName, "", "Name of the app to bootstrap")
	cmd.Flags().StringVar(&f.UpstreamRepo, flagUpstreamRepo, "", "URL of the upstream repository containing the Helm chart")
	cmd.Flags().StringVar(&f.UpstreamChart, flagUpstreamChart, "", "Path to the Helm chart in the upstream repository")
	cmd.Flags().StringVar(&f.Team, flagTeam, "", "Team responsible for the app")
	cmd.Flags().StringVar(&f.SyncMethod, flagSyncMethod, methodVendir, "Method to sync upstream chart (vendir or kustomize)")
	cmd.Flags().StringVar(&f.PatchMethod, flagPatchMethod, methodScript, "Method to patch upstream chart (script or kustomize)")
	cmd.Flags().StringVar(&f.GithubToken, flagGithubToken, "GITHUB_TOKEN", "Name of environment variable containing GitHub token")
	cmd.Flags().BoolVar(&f.DryRun, flagDryRun, false, "If set, only print what would be done")

	// Check errors from MarkFlagRequired
	if err := cmd.MarkFlagRequired(flagName); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired(flagUpstreamRepo); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired(flagUpstreamChart); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired(flagTeam); err != nil {
		panic(err)
	}
}

func (f *flag) Validate() error {
	if f.Name == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagName)
	}
	if f.UpstreamRepo == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagUpstreamRepo)
	}
	if f.UpstreamChart == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagUpstreamChart)
	}
	if f.Team == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagTeam)
	}
	if f.SyncMethod != methodVendir && f.SyncMethod != methodKustomize {
		return microerror.Maskf(invalidFlagError, "--%s must be either '%s' or '%s'", flagSyncMethod, methodVendir, methodKustomize)
	}
	if f.PatchMethod != methodScript && f.PatchMethod != methodKustomize {
		return microerror.Maskf(invalidFlagError, "--%s must be either '%s' or '%s'", flagPatchMethod, methodScript, methodKustomize)
	}

	return nil
}
