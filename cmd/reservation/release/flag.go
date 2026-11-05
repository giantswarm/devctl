package release

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
	flagCluster     = "cluster"
	flagPullRequest = "pull-request"
	flagUser        = "user"
)

type flag struct {
	App         string
	AppDir      string
	Cluster     string
	PullRequest string
	GitOps      gitops.Flags
	User        string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.App, flagApp, "", "Chart of the collection app to release. Empty takes the name from the app repository's helm/*/Chart.yaml, as --app-dir points at. Not with --app-dir.")
	cmd.Flags().StringVar(&f.AppDir, flagAppDir, "", "Checkout of the app repository, read only to take the chart name when --app is empty. Empty is the current directory.")
	cmd.Flags().StringVar(&f.PullRequest, flagPullRequest, "", "Pull request whose reservations to release, as owner/repo#number. Without --cluster, it releases every reservation that pull request holds, on every enabled cluster: a merged or closed pull request knows neither cluster nor app. Required without --cluster.")
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to release the app on. Empty releases by --pull-request on every enabled cluster.")
	f.GitOps.Init(cmd)
	f.GitOps.InitDryRun(cmd)
	cmd.Flags().StringVar(&f.User, flagUser, "", gitops.UserHelp("recorded in the release commit message. It need not be the original holder"))
}

func (f *flag) Validate() error {
	if err := f.GitOps.Validate(); err != nil {
		return microerror.Mask(err)
	}

	if f.Cluster == "" && f.PullRequest == "" {
		return microerror.Maskf(invalidFlagError,
			"pass --%s, or --%s to release every reservation a pull request holds", flagCluster, flagPullRequest)
	}

	if f.Cluster != "" {
		if err := validate.Name("--"+flagCluster, f.Cluster); err != nil {
			return microerror.Maskf(invalidFlagError, "%v", err)
		}
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

	// --cluster picks the form. With one, this releases that cluster's app,
	// and --pull-request restricts it to a reservation that pull request
	// holds. Without one, --pull-request releases every reservation the pull
	// request holds, wherever it holds one, so it takes no app.
	if f.Cluster == "" {
		for _, r := range []struct{ name, value string }{
			{flagApp, f.App},
			{flagAppDir, f.AppDir},
		} {
			if r.value != "" {
				return microerror.Maskf(invalidFlagError,
					"--%s without --%s releases every reservation the pull request holds, so it takes no --%s",
					flagPullRequest, flagCluster, r.name)
			}
		}
	}
	if f.App != "" && f.AppDir != "" {
		return microerror.Maskf(invalidFlagError, "--%s and --%s are mutually exclusive: --%s is read only to take the chart name", flagApp, flagAppDir, flagAppDir)
	}

	return nil
}
