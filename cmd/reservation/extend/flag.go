package extend

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagRepoDir     = "repo-dir"
	flagPullRequest = "pull-request"
	flagUser        = "user"
)

type flag struct {
	RepoDir     string
	PullRequest string
	User        string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.RepoDir, flagRepoDir, ".", "Checkout of the GitOps repository to sweep. The command commits to it and pushes; it never clones.")
	cmd.Flags().StringVar(&f.PullRequest, flagPullRequest, "", "Pull request whose reservations to extend, as owner/repo#number.")
	cmd.Flags().StringVar(&f.User, flagUser, "", "GitHub login attributed to each extension commit. It need not be the original holder.")
}

func (f *flag) Validate() error {
	for _, r := range []struct{ name, value string }{
		{flagRepoDir, f.RepoDir},
		{flagPullRequest, f.PullRequest},
		{flagUser, f.User},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}

	return nil
}
