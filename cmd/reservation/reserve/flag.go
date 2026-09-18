package reserve

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reservation"
)

const (
	flagApp         = "app"
	flagAppDir      = "app-dir"
	flagBranch      = "branch"
	flagCluster     = "cluster"
	flagDuration    = "duration"
	flagExclusive   = "exclusive"
	flagGitOpsRepo  = "gitops-repo"
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
	GitOpsRepo  string
	PullRequest string
	User        string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.App, flagApp, "", "Chart of the collection app to reserve. Empty takes the name from the app repository's helm/*/Chart.yaml. Required when that repository holds several charts, and also the override when a chart is named differently from the chart its OCI URL serves.")
	cmd.Flags().StringVar(&f.AppDir, flagAppDir, ".", "Checkout of the app repository, read only to take the chart name when --app is empty.")
	cmd.Flags().StringVar(&f.Branch, flagBranch, "", "Branch of the app repository whose dev builds the cluster follows.")
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to reserve the app on.")
	cmd.Flags().StringVar(&f.Duration, flagDuration, "", "How long the reservation lasts, as 30m, 4h or 2d. Empty is the default of 10h. The maximum is 7d, or less when the management cluster sets its own.")
	cmd.Flags().BoolVar(&f.Exclusive, flagExclusive, false, "Lock the whole cluster instead of just --app: the reservation fails against any other active reservation, and succeeds as a promotion in place when the only one active belongs to the same user and app.")
	cmd.Flags().StringVar(&f.GitOpsRepo, flagGitOpsRepo, "", "GitOps repository holding the management cluster, as owner/repo.")
	cmd.Flags().StringVar(&f.PullRequest, flagPullRequest, "", "Pull request the reservation belongs to, as owner/repo#number.")
	cmd.Flags().StringVar(&f.User, flagUser, "", "GitHub login of the person holding the reservation.")
}

func (f *flag) Validate() error {
	for _, r := range []struct{ name, value string }{
		{flagBranch, f.Branch},
		{flagCluster, f.Cluster},
		{flagGitOpsRepo, f.GitOpsRepo},
		{flagUser, f.User},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}
	if f.App == "" && f.AppDir == "" {
		return microerror.Maskf(invalidFlagError,
			"pass --%s, or --%s pointing at a checkout of the app repository: the chart name is not the repository name", flagApp, flagAppDir)
	}
	if _, _, err := splitRepo(f.GitOpsRepo); err != nil {
		return microerror.Mask(err)
	}
	// Parsed here rather than after the clone: a wrong duration is a typo, and a
	// typo should not cost a clone of a GitOps repo to find out about.
	if _, err := reservation.ParseDuration(f.Duration); err != nil {
		return microerror.Mask(err)
	}

	return nil
}

// splitRepo splits an owner/repo reference.
func splitRepo(repo string) (string, string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", microerror.Maskf(invalidFlagError, "--%s must be owner/repo, got %q", flagGitOpsRepo, repo)
	}

	return owner, name, nil
}
