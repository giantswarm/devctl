package kubeconfig

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	clusterID  = "clusterID"
	kubeconfig = "kubeconfig"
)

type flag struct {
	ClusterID  string
	Kubeconfig string
	Provider   string
	Output     string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.ClusterID, clusterID, "", `ID of the tenant cluster that you want to connect to.`)
	cmd.Flags().StringVar(&f.Kubeconfig, kubeconfig, "", `Path to control plane kubeconfig`)
	cmd.Flags().StringVar(&f.Provider, "provider", "azure", `Path to control plane kubeconfig`)
	cmd.Flags().StringVar(&f.Output, "output", "", `Where you want the generated kubeconfig file to be saved`)
}

func (f *flag) Validate() error {
	if f.ClusterID == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", clusterID)
	}
	if f.Kubeconfig == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", kubeconfig)
	}

	return nil
}
