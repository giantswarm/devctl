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
	// image pipeline (push-to-registries with split-china-push and the
	// paired sync-china-registry job).
	HasDockerfile bool
	// HasApp is true when the repo carries the "app" flavour (at least one
	// Helm chart). It selects the chart pipeline (push-to-app-catalog with the
	// app-build-suite executor and run-tests-with-ats).
	HasApp bool
	// BranchPublish is true when the repo opts into publishing a dev image and
	// chart on branch builds. By default branches build + test only; when set,
	// the branch path additionally pushes an amd64 dev image and the dev chart
	// (coupled).
	BranchPublish bool
	// ReleaseBinaries is true when the repo distributes cross-platform Go
	// binaries on its GitHub Release. It adds the six-platform architectures
	// matrix to go-build and an upload-release-assets job, and caps the
	// multi-arch image push to linux/amd64,linux/arm64 (otherwise buildx tries
	// the darwin/windows targets under QEMU and hangs).
	ReleaseBinaries bool
	// OrbVersion is the giantswarm/architect orb version to pin.
	OrbVersion string
}
