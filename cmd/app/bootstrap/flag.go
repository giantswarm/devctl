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
	flagGithubToken   = "github-token-envvar"
	flagDryRun        = "dry-run"
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
	cmd.Flags().StringVar(&f.SyncMethod, flagSyncMethod, "vendir", "Method to sync upstream chart (vendir or kustomize)")
	cmd.Flags().StringVar(&f.PatchMethod, flagPatchMethod, "script", "Method to patch upstream chart (script or kustomize)")
	cmd.Flags().StringVar(&f.GithubToken, flagGithubToken, "GITHUB_TOKEN", "Name of environment variable containing GitHub token")
	cmd.Flags().BoolVar(&f.DryRun, flagDryRun, false, "If set, only print what would be done")

	cmd.MarkFlagRequired(flagName)
	cmd.MarkFlagRequired(flagUpstreamRepo)
	cmd.MarkFlagRequired(flagUpstreamChart)
	cmd.MarkFlagRequired(flagTeam)
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
	if f.SyncMethod != "vendir" && f.SyncMethod != "kustomize" {
		return microerror.Maskf(invalidFlagError, "--%s must be either 'vendir' or 'kustomize'", flagSyncMethod)
	}
	if f.PatchMethod != "script" && f.PatchMethod != "kustomize" {
		return microerror.Maskf(invalidFlagError, "--%s must be either 'script' or 'kustomize'", flagPatchMethod)
	}

	return nil
}
