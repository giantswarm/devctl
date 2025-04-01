package deploy

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
	// GitOps repository configuration
	GitOpsRepo   string
	GitOpsBranch string

	// Cluster configuration
	ManagementCluster string
	Organization      string
	WorkloadCluster   string

	// Application configuration
	AppName      string
	AppVersion   string
	AppCatalog   string
	AppNamespace string

	// Deployment configuration
	Timeout int
}

func (f *flag) Init(cmd *cobra.Command) {
	// GitOps repository flags
	cmd.Flags().StringVar(&f.GitOpsRepo, "gitops-repo", "giantswarm/workload-clusters-fleet", "GitOps repository (owner/repo)")

	// Cluster flags
	cmd.Flags().StringVar(&f.ManagementCluster, "management-cluster", "gazelle", "Name of the management cluster")
	cmd.Flags().StringVar(&f.Organization, "organization", "giantswarm", "Name of the organization")
	cmd.Flags().StringVar(&f.WorkloadCluster, "workload-cluster", "operations", "Name of the workload cluster")

	// Application flags
	cmd.Flags().StringVar(&f.AppName, "app-name", "", "Name of the application to deploy")
	cmd.Flags().StringVar(&f.AppCatalog, "app-catalog", "giantswarm", "Name of the application catalog")
	cmd.Flags().StringVar(&f.AppVersion, "app-version", "", "Version of the application to deploy")
	cmd.Flags().StringVar(&f.AppNamespace, "target-namespace", "default", "Kubernetes namespace to deploy the application to")

	// Deployment flags
	cmd.Flags().IntVar(&f.Timeout, "timeout", 300, "Timeout in seconds to wait for deployment")
}

func (f *flag) Validate() error {
	if f.AppName == "" {
		return microerror.Maskf(invalidConfigError, "app name must not be empty")
	}
	if f.AppVersion == "" {
		return microerror.Maskf(invalidConfigError, "app version must not be empty")
	}
	return nil
}
