package release

// Provider names, as they appear in the releases repository directory layout
// and in the `--provider` flag.
const (
	providerAWS           = "aws"
	providerAzure         = "azure"
	providerEKS           = "eks"
	providerVSphere       = "vsphere"
	providerCloudDirector = "cloud-director"
)

// Release types, derived from the base and new versions.
const (
	releaseTypePatch = "patch"
	releaseTypeMinor = "minor"
)

// Output formats accepted by `--output`.
const (
	outputText     = "text"
	outputMarkdown = "markdown"
)

// Component names this package treats specially. containerdComponentName and
// osToolingComponentName live in containerd.go beside the code that derives one
// from the other.
const (
	kubernetesComponentName = "kubernetes"
	clusterComponentName    = "cluster"
)

// The upstream Kubernetes repository the release bumper reads tags from.
const (
	kubernetesGitHubOwner = "kubernetes"
	kubernetesGitHubRepo  = "kubernetes"
)
