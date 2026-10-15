package list

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/reservation/gitops"
)

const (
	flagCluster = "cluster"
)

type flag struct {
	Cluster string
	GitOps  gitops.Flags
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to list reservations on.")
	f.GitOps.Init(cmd)
}

func (f *flag) Validate() error {
	if err := f.GitOps.Validate(); err != nil {
		return microerror.Mask(err)
	}

	for _, r := range []struct{ name, value string }{
		{flagCluster, f.Cluster},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}

	return nil
}
