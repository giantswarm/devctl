package list

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagCluster = "cluster"
	flagRepoDir = "repo-dir"
)

type flag struct {
	Cluster string
	RepoDir string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to list reservations on.")
	cmd.Flags().StringVar(&f.RepoDir, flagRepoDir, ".", "Checkout of the GitOps repository holding the management cluster.")
}

func (f *flag) Validate() error {
	for _, r := range []struct{ name, value string }{
		{flagCluster, f.Cluster},
		{flagRepoDir, f.RepoDir},
	} {
		if r.value == "" {
			return microerror.Maskf(invalidFlagError, "--%s must not be empty", r.name)
		}
	}

	return nil
}
