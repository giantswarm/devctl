package list

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
	"github.com/giantswarm/devctl/v8/internal/validate"
)

const (
	flagCluster = "cluster"
)

type flag struct {
	Cluster string
	GitOps  gitops.Flags
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to list reservations on. Required.")
	f.GitOps.Init(cmd)
}

func (f *flag) Validate() error {
	if err := f.GitOps.Validate(); err != nil {
		return microerror.Mask(err)
	}

	if f.Cluster == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagCluster)
	}

	if err := validate.Name("--"+flagCluster, f.Cluster); err != nil {
		return microerror.Maskf(invalidFlagError, "%v", err)
	}

	return nil
}
