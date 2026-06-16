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
	// AppCatalog is the catalog the chart pipeline publishes to (the
	// push-to-app-catalog `app_catalog` param). Defaults to "giantswarm-catalog".
	// Repos that ship to a different catalog (e.g. the internal
	// "giantswarm-operations-platform") set it so generation does not silently
	// migrate their chart to the public catalog.
	AppCatalog string
	// AppCatalogTest is the test catalog the chart pipeline publishes to (the
	// push-to-app-catalog `app_catalog_test` param). Defaults to
	// "giantswarm-test-catalog". Kept paired with AppCatalog.
	AppCatalogTest string
	// BranchPublish is true when the repo opts into publishing a dev image and
	// chart on branch builds. By default branches build + test only; when set,
	// the branch path additionally pushes an amd64 dev image and the dev chart
	// (coupled).
	BranchPublish bool
	// ImagePreBuildJob names a repo-owned job (defined in .circleci/custom.yml)
	// that the release image build must wait on. The generated
	// push-to-registries-release job gains a `requires` entry for it, which the
	// append-only custom.yml merge cannot inject into a generated job. Used for
	// workspace-handoff pre-steps (e.g. a job that persists a generated file the
	// Docker build context overlays via attach_workspace). Empty for the common
	// case.
	ImagePreBuildJob string
	// ImagePrivateOnly is true when the repo's image must ship only to the
	// private registry (gsociprivate). It replaces the default split-china-push
	// (which also publishes the public gsoci copy and mirrors to Aliyun) with an
	// explicit private-only registries-data, and omits the sync-china-registry
	// job. Set it for private repos whose image must not land in the public
	// catalog.
	ImagePrivateOnly bool
	// ReleaseBinaries is true when the repo distributes cross-platform Go
	// binaries on its GitHub Release (derived from the "cli" flavour on a Go
	// repo). It adds the six-platform architectures matrix to go-build and an
	// upload-release-assets job, and caps the multi-arch image push to
	// linux/amd64,linux/arm64 (otherwise buildx tries the darwin/windows
	// targets under QEMU and hangs).
	ReleaseBinaries bool
	// OrbVersion is the giantswarm/architect orb version to pin.
	OrbVersion string
	// ContinuationOrbVersion is the circleci/continuation orb version the
	// setup config pins.
	ContinuationOrbVersion string
}
