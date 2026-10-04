package rolloutwait

import (
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
)

// The verdicts of a rollout wait beyond the shared table.
const (
	VerdictRolledOut     agentcli.Verdict = "rolled_out"
	VerdictRolloutFailed agentcli.Verdict = "rollout_failed"
)

// The kinds of object that deploy a chart on an installation.
const (
	KindHelmRelease = "HelmRelease"
	KindApp         = "App"
)

// The states of one deployment.
const (
	// StateRolledOut: the version (or a newer one) is deployed, the object
	// is ready and its workloads are rolled out.
	StateRolledOut = "rolled_out"
	// StateProgressing: not there yet; the message says what is missing.
	StateProgressing = "progressing"
	// StateFailed: the version was attempted and failed.
	StateFailed = "failed"
	// StateNotFollowing: the object's source pins a version or a range that
	// excludes the version; it will never get there on its own.
	StateNotFollowing = "not_following"
	// StateSuspended: Flux does not reconcile the object.
	StateSuspended = "suspended"
)

// Result is what the rollout wait found on the installation.
type Result struct {
	// Installation is the installation waited on.
	Installation string `json:"installation"`
	// Context is the kube context the installation was read through.
	Context string `json:"context"`
	// Version is the bare version waited for; empty for a revision.
	Version string `json:"version"`
	// Revision is the merge commit of a configuration change waited for;
	// empty for a version.
	Revision string `json:"revision,omitempty"`
	// Charts are the charts of the release, the names deployments are
	// matched by.
	Charts []string `json:"charts"`
	// Deployments are the HelmReleases and App CRs that deploy one of the
	// charts, as of the last poll.
	Deployments []Deployment `json:"deployments"`
}

// NewResult is the result before anything is known.
func NewResult(installation, context string) Result {
	return Result{Installation: installation, Context: context, Charts: []string{}, Deployments: []Deployment{}}
}

// Deployment is one HelmRelease or App CR that deploys a chart of the
// release, and where it stands.
type Deployment struct {
	// Kind is HelmRelease or App; for a revision also GitRepository,
	// Kustomization or Konfiguration.
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Chart     string `json:"chart"`
	// Source is where the chart comes from: "OCIRepository <ns>/<name>",
	// "HelmChart <ns>/<name>" or "catalog <name>".
	Source string `json:"source"`
	// Follows is the version selector of the source: "semver <range>",
	// "tag <tag>", "digest <digest>" or, for an App, "version <v>".
	Follows string `json:"follows"`
	// RunningVersion is the deployed chart version without build
	// metadata; empty before the first deployment.
	RunningVersion string `json:"runningVersion"`
	// State is rolled_out, progressing, failed, not_following or suspended.
	State string `json:"state"`
	// Message says why the deployment is not rolled out; empty when it is.
	Message string `json:"message"`
	// Workloads are the Deployments, StatefulSets and DaemonSets of the
	// Helm release, read once the version is deployed.
	Workloads []Workload `json:"workloads"`

	// source is the Flux source object a reconcile request goes to; nil for
	// an App and for a HelmChart that does not exist yet.
	source *object
	// self is, for a revision, the object itself a reconcile request goes
	// to; nil for what Flux does not reconcile on request (a Konfiguration).
	self *object
}

// Workload is one Deployment, StatefulSet or DaemonSet of a release.
type Workload struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     bool   `json:"ready"`
	// Message says what the rollout still waits for; empty when ready.
	Message string `json:"message"`
}

// Document is the command's JSON: the envelope, the release the rollout
// waited for first, and the rollout.
type Document struct {
	agentcli.Envelope
	// Release is the release wait's outcome and result.
	Release prmerge.Release `json:"release"`
	Result
}
