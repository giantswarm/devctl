package wait

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

type flag struct {
	PR             int
	Context        string
	Timeout        time.Duration
	ReleaseTimeout time.Duration
	Reconcile      bool
	Images         []string
	Progress       bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().IntVar(&f.PR, "pr", 0, "Wait for the release auto-release tagged from this merged pull request instead of a version")
	cmd.Flags().StringVar(&f.Context, "context", "", "The kube context of the installation's management cluster (default "+rolloutwait.ContextPrefix+"<installation>)")
	cmd.Flags().DurationVar(&f.Timeout, "timeout", rolloutwait.DefaultTimeout, "How long to wait for the rollout once the release is available before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().DurationVar(&f.ReleaseTimeout, "release-timeout", releasewait.DefaultTimeout, "How long to wait for the release to be available first before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().BoolVar(&f.Reconcile, "reconcile", false, "Ask Flux to reconcile the sources and HelmReleases that are behind once (sets reconcile.fluxcd.io/requestedAt), instead of waiting for their interval")
	cmd.Flags().StringSliceVar(&f.Images, "image", nil, releasewait.ImageFlagUsage)
	agentcli.ProgressFlag(cmd, &f.Progress)
}
