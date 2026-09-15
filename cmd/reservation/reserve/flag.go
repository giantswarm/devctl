package reserve

import (
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagApp         = "app"
	flagBranch      = "branch"
	flagCluster     = "cluster"
	flagGitOpsRepo  = "gitops-repo"
	flagPullRequest = "pull-request"
	flagUser        = "user"
)

type flag struct {
	App         string
	Branch      string
	Cluster     string
	GitOpsRepo  string
	PullRequest string
	User        string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.App, flagApp, "", "Name of the collection app to reserve.")
	cmd.Flags().StringVar(&f.Branch, flagBranch, "", "Branch of the app repository whose dev builds the cluster follows.")
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to reserve the app on.")
	cmd.Flags().StringVar(&f.GitOpsRepo, flagGitOpsRepo, "", "GitOps repository holding the management cluster, as owner/repo.")
	cmd.Flags().StringVar(&f.PullRequest, flagPullRequest, "", "Pull request the reservation belongs to, as owner/repo#number.")
	cmd.Flags().StringVar(&f.User, flagUser, "", "GitHub login of the person holding the reservation.")
}

func (f *flag) Validate() error {
	for _, r := range []struct{ name, value string }{
		{flagApp, f.App},
		{flagBranch, f.Branch},
		{flagCluster, f.Cluster},
		{flagGitOpsRepo, f.GitOpsRepo},
		{flagUser, f.User},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}
	if _, _, err := splitRepo(f.GitOpsRepo); err != nil {
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
