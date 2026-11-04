package wait

import (
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

type flag struct {
	Version        string
	PR             int
	Context        string
	Kubeconfig     string
	Timeout        time.Duration
	ReleaseTimeout time.Duration
	Reconcile      bool
	Images         []string
	Charts         []string
	HelmReleases   []string
	FailedLog      prwait.FailedLog
	Progress       bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Version, "version", "", "The release's version, vX.Y.Z or X.Y.Z: the same as the version argument")
	cmd.Flags().IntVar(&f.PR, "pr", 0, "Wait for the release auto-release tagged from this merged pull request instead of a version")
	cmd.Flags().StringVar(&f.Context, "context", "", "The kube context of the installation's management cluster (default "+rolloutwait.ContextPrefix+"<installation>)")
	cmd.Flags().StringVar(&f.Kubeconfig, "kubeconfig", "", "Read the kube context from this kubeconfig file only, instead of KUBECONFIG or ~/.kube/config; it is never written")
	cmd.Flags().DurationVar(&f.Timeout, "timeout", rolloutwait.DefaultTimeout, "How long to wait for the rollout once the release is available before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().DurationVar(&f.ReleaseTimeout, "release-timeout", releasewait.DefaultTimeout, "How long to wait for the release to be available first before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().BoolVar(&f.Reconcile, "reconcile", false, "Ask Flux to reconcile the sources and HelmReleases that are behind once (sets reconcile.fluxcd.io/requestedAt), instead of waiting for their interval")
	cmd.Flags().StringSliceVar(&f.Images, "image", nil, releasewait.ImageFlagUsage)
	cmd.Flags().StringSliceVar(&f.Charts, "chart", nil, "A bare chart name waits directly for that component's version and readiness, skipping the repository release wait; one chart, with a version and without --pr, --image or --failed-log. A path keeps the release wait: "+releasewait.ChartFlagUsage)
	cmd.Flags().StringSliceVar(&f.HelmReleases, "helmrelease", nil, "Also wait for this HelmRelease, <name> or <namespace>/<name>, whatever chart it deploys (repeatable); exit 3 when none answers to the name")
	f.FailedLog.Init(cmd)
	agentcli.ProgressFlag(cmd, &f.Progress)
}

// componentChart distinguishes the component form from the existing OCI
// paths, which add artifacts to the repository's release wait.
func (f *flag) componentChart() (string, error) {
	for _, chart := range f.Charts {
		if strings.Contains(chart, "/") {
			continue
		}
		if len(validation.IsDNS1123Subdomain(chart)) != 0 {
			return "", agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "--chart %q: name a component as --chart <chart-name> <version>, or a release chart by its OCI path (e.g. giantswarm/kagent/helm/kagent)", chart)
		}
		if len(f.Charts) != 1 || f.PR != 0 || len(f.Images) != 0 || f.FailedLog.Enabled {
			return "", agentcli.NewExitError(agentcli.ExitUsage, agentcli.VerdictUsage, "the component form --chart <chart-name> <version> takes one chart and no --pr, --image or --failed-log: the repository release is not checked")
		}
		return chart, nil
	}
	return "", nil
}
