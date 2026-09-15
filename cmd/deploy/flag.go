package deploy

import (
	"maps"
	"slices"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/internal/validate"
)

type flag struct {
	// GitOps repository flags
	GitOpsRepo string

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
	Timeout time.Duration
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
	cmd.Flags().DurationVar(&f.Timeout, "timeout", 300*time.Second, "Timeout in seconds to wait for deployment")
}

func (f *flag) Validate() error {
	// Every value below is interpolated into the argument list of kubectl, so
	// each is constrained to an identifier and cannot be read as a flag.
	names := map[string]string{
		"--app-name":           f.AppName,
		"--app-catalog":        f.AppCatalog,
		"--target-namespace":   f.AppNamespace,
		"--management-cluster": f.ManagementCluster,
		"--organization":       f.Organization,
		"--workload-cluster":   f.WorkloadCluster,
	}
	for _, kind := range slices.Sorted(maps.Keys(names)) {
		if err := validate.Name(kind, names[kind]); err != nil {
			return microerror.Maskf(invalidConfigError, "%s", err)
		}
	}
	if err := validate.Version("--app-version", f.AppVersion); err != nil {
		return microerror.Maskf(invalidConfigError, "%s", err)
	}

	return nil
}
