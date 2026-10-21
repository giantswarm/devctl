package reap

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/reservation"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	flagUser = "user"
)

type flag struct {
	GitOps gitops.Flags
	User   string
}

func (f *flag) Init(cmd *cobra.Command) {
	f.GitOps.Init(cmd)
	cmd.Flags().StringVar(&f.User, flagUser, "", "GitHub login recorded in each release commit message. It need not be the original holder.")
}

func (f *flag) Validate() error {
	if err := f.GitOps.Validate(); err != nil {
		return microerror.Mask(err)
	}

	if f.User != "" {
		if reservation.ValidateUser(f.User) != nil {
			return microerror.Maskf(invalidFlagError, "--%s %q is not a GitHub login", flagUser, f.User)
		}
	}

	return nil
}
