package list

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/rolloutwait"
)

const (
	flagCluster = "cluster"
	flagContext = "context"
)

type flag struct {
	Cluster string
	Context string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to list reservations on.")
	cmd.Flags().StringVar(&f.Context, flagContext, "", "Kube context of the management cluster. Empty is "+rolloutwait.ContextPrefix+"<cluster>, the context `tsh kube login <cluster>` writes.")
}

func (f *flag) Validate() error {
	if f.Cluster == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagCluster)
	}

	return nil
}
