package reserve

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
	"github.com/giantswarm/devctl/v8/internal/validate"
	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const (
	flagApp         = "app"
	flagAppDir      = "app-dir"
	flagBranch      = "branch"
	flagCluster     = "cluster"
	flagDuration    = "duration"
	flagExclusive   = "exclusive"
	flagPullRequest = "pull-request"
	flagUser        = "user"
)

type flag struct {
	App         string
	AppDir      string
	Branch      string
	Cluster     string
	Duration    string
	Exclusive   bool
	GitOps      gitops.Flags
	PullRequest string
	User        string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.App, flagApp, "", "Chart of the collection app to reserve. Empty takes the name from the app repository's helm/*/Chart.yaml. Required when that repository holds several charts, and also the override when a chart is named differently from the chart its OCI URL serves. Not with --app-dir.")
	cmd.Flags().StringVar(&f.AppDir, flagAppDir, "", "Checkout of the app repository, read only to take the chart name when --app is empty. Empty is the current directory.")
	cmd.Flags().StringVar(&f.Branch, flagBranch, "", "Branch of the app repository whose dev builds the cluster follows. Required.")
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to reserve the app on. Required.")
	cmd.Flags().StringVar(&f.Duration, flagDuration, "", "How long the reservation lasts, as 30m, 4h, 4h30m or 2d. Empty is the default of 10h, or the cluster maximum when that is lower. The maximum is 7d, or less when the management cluster sets its own.")
	cmd.Flags().BoolVar(&f.Exclusive, flagExclusive, false, "Lock the whole cluster instead of just --app: the reservation fails against any other active reservation, and succeeds as a promotion in place when the only one active belongs to the same user and app.")
	f.GitOps.Init(cmd)
	f.GitOps.InitDryRun(cmd)
	cmd.Flags().StringVar(&f.PullRequest, flagPullRequest, "", "Pull request the reservation belongs to, as owner/repo#number. Optional: omit it for a branch built some other way. Without it devctl does not check that a pull request exists for --branch, and extend --pull-request, release --pull-request and the reap rename check do not find this reservation.")
	cmd.Flags().StringVar(&f.User, flagUser, "", gitops.UserHelp("of the person who holds the reservation"))
}

func (f *flag) Validate() error {
	if err := f.GitOps.Validate(); err != nil {
		return microerror.Mask(err)
	}

	for _, r := range []struct{ name, value string }{
		{flagBranch, f.Branch},
		{flagCluster, f.Cluster},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}

	if err := validate.Name("--"+flagCluster, f.Cluster); err != nil {
		return microerror.Maskf(invalidFlagError, "%v", err)
	}
	if f.User != "" {
		if reservation.ValidateUser(f.User) != nil {
			return microerror.Maskf(invalidFlagError, "--%s %q is not a GitHub login", flagUser, f.User)
		}
	}
	if f.PullRequest != "" {
		if _, _, _, err := reservation.SplitPullRequest(f.PullRequest); err != nil {
			return microerror.Maskf(invalidFlagError, "--%s %q is not owner/repo#number", flagPullRequest, f.PullRequest)
		}
	}
	// Parsed here before the sync: a wrong duration is a typo, and a typo should
	// not cost a fetch from the GitOps repo to find out about.
	if _, err := reservation.ParseDuration(f.Duration); err != nil {
		return microerror.Maskf(invalidFlagError, "--%s %q is not a duration. Accepted forms are %s", flagDuration, f.Duration, reservation.AcceptedDurations)
	}

	if f.App != "" && f.AppDir != "" {
		return microerror.Maskf(invalidFlagError, "--%s and --%s are mutually exclusive: --%s is read only to take the chart name", flagApp, flagAppDir, flagAppDir)
	}

	return nil
}
