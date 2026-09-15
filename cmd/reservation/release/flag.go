package release

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagApp     = "app"
	flagAppDir  = "app-dir"
	flagCluster = "cluster"
	flagRepoDir = "repo-dir"
	flagUser    = "user"
)

type flag struct {
	App     string
	AppDir  string
	Cluster string
	RepoDir string
	User    string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.App, flagApp, "", "Chart of the collection app to release. Empty takes the name from the app repository's helm/*/Chart.yaml, as --app-dir points at. Reserve's Result.App is always safe to pass here, since a reservation is keyed on the resolved chart.")
	cmd.Flags().StringVar(&f.AppDir, flagAppDir, "", "Checkout of the app repository, read only to take the chart name when --app is empty.")
	cmd.Flags().StringVar(&f.Cluster, flagCluster, "", "Name of the management cluster to release the app on.")
	cmd.Flags().StringVar(&f.RepoDir, flagRepoDir, ".", "Checkout of the GitOps repository holding the management cluster. The command commits to it and pushes; it never clones.")
	cmd.Flags().StringVar(&f.User, flagUser, "", "GitHub login of whoever releases the reservation. It need not be the original holder.")
}

func (f *flag) Validate() error {
	for _, r := range []struct{ name, value string }{
		{flagCluster, f.Cluster},
		{flagRepoDir, f.RepoDir},
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

	return nil
}
