package deploy

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

type flag struct {
	// GitOps repository configuration
	GitOpsRepo     string
	GitOpsBranch   string
	GithubTokenEnv string

	// Cluster configuration
	ManagementCluster string
	Organization      string
	WorkloadCluster   string

	// Application configuration
	AppName      string
	AppCatalog   string
	AppVersion   string
	AppNamespace string

	// Deployment configuration
	Timeout      int
	PollInterval int
	DryRun       bool
}

func (f *flag) Init(cmd *cobra.Command) {
	// GitOps repository fs
	cmd.Flags().StringVar(&f.GitOpsRepo, "gitops-repo", "", "GitOps repository in format owner/repo")
	cmd.Flags().StringVar(&f.GitOpsBranch, "gitops-branch", "main", "GitOps repository branch to create PR against")
	cmd.Flags().StringVar(&f.GithubTokenEnv, "github-token-envvar", "GITHUB_TOKEN", "Environment variable name for Github token")

	// Cluster flags
	cmd.Flags().StringVar(&f.ManagementCluster, "management-cluster", "gazelle", "Name of the management cluster")
	cmd.Flags().StringVar(&f.Organization, "organization", "giantswarm", "Name of the organization")
	cmd.Flags().StringVar(&f.WorkloadCluster, "workload-cluster", "operations", "Name of the workload cluster")

	// Application flags
	cmd.Flags().StringVar(&f.AppName, "app", "", "Name of the application to deploy")
	cmd.Flags().StringVar(&f.AppCatalog, "catalog", "giantswarm", "Name of the application catalog")
	cmd.Flags().StringVar(&f.AppVersion, "version", "", "Version of the application to deploy")
	cmd.Flags().StringVar(&f.AppNamespace, "target-namespace", "default", "Kubernetes namespace to deploy the application to")

	// Deployment flags
	cmd.Flags().IntVar(&f.Timeout, "timeout", 300, "Timeout in seconds to wait for deployment")
	cmd.Flags().IntVar(&f.PollInterval, "poll-interval", 10, "Interval in seconds between deployment status checks")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Show what would be done without making changes")

	// Required flags
	cmd.MarkFlagRequired("app")
	cmd.MarkFlagRequired("gitops-repo")
	cmd.MarkFlagRequired("version")
}

func (f *flag) Validate() error {
	if f.AppName == "" {
		return microerror.Maskf(invalidConfigError, "app name must not be empty")
	}
	if f.GitOpsRepo == "" {
		return microerror.Maskf(invalidConfigError, "gitops repository must not be empty")
	}
	if f.AppVersion == "" {
		return microerror.Maskf(invalidConfigError, "app version must not be empty")
	}
	return nil
}
