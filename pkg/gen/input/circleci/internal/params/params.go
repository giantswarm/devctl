package params

// Params carries the derived signals that determine which jobs the CircleCI
// config contains. Nothing here is a free-form CI parameter block: every field
// is derived from existing devctl gen signals (language, flavours) or from repo
// content (Dockerfile presence), per the CircleCI flavor model.
type Params struct {
	// RepoName is the repository name. It is used for the Go binary, the
	// Helm chart, and the architect job names.
	RepoName string
	// Language is the repo language (e.g. "go"). "go" selects the go-build
	// job.
	Language string
	// HasDockerfile is true when the repo ships a Dockerfile. It selects the
	// image pipeline (push-to-registries multiarch + split-china-push and the
	// paired sync-china-registry job).
	HasDockerfile bool
	// HasApp is true when the repo carries the "app" flavour (at least one
	// Helm chart). It selects the chart pipeline (push-to-app-catalog with the
	// app-build-suite executor and run-tests-with-ats).
	HasApp bool
	// OrbVersion is the giantswarm/architect orb version to pin.
	OrbVersion string
}
