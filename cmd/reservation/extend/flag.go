package extend

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	flagPullRequest = "pull-request"
	flagUser        = "user"
)

type flag struct {
	GitOps      gitops.Flags
	PullRequest string
	User        string
}

func (f *flag) Init(cmd *cobra.Command) {
	f.GitOps.Init(cmd)
	cmd.Flags().StringVar(&f.PullRequest, flagPullRequest, "", "Pull request whose reservations to extend, as owner/repo#number.")
	cmd.Flags().StringVar(&f.User, flagUser, "", "GitHub login attributed to each extension commit. It need not be the original holder.")
}

func (f *flag) Validate() error {
	if err := f.GitOps.Validate(); err != nil {
		return microerror.Mask(err)
	}

	for _, r := range []struct{ name, value string }{
		{flagPullRequest, f.PullRequest},
		{flagUser, f.User},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}

	return nil
}
