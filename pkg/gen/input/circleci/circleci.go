package circleci

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/file"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci/internal/params"
)

// OrbVersion is the aligned giantswarm/architect orb version every generated
// CircleCI config pins. It is baked in next to the template -- not a flag and
// not passed in by callers -- so that an orb bump (which can change the
// template's required job/param shape, i.e. a cross-major compatibility
// contract) forces a new devctl release rather than silently combining a stale
// template with a newer orb at generation time.
//
// Renovate keeps this current; a major bump lands as a devctl PR, gets released,
// and only then reaches repos via the align-files devctl pin.
//
// renovate: datasource=orb depName=giantswarm/architect
const OrbVersion = "9.5.3"

// ContinuationOrbVersion pins the circleci/continuation orb used by the
// generated setup config (.circleci/config.yml) to merge the optional
// repo-owned .circleci/custom.yml into .circleci/workflows.yml at pipeline
// runtime. Baked in for the same reason as OrbVersion: a bump ships with a
// devctl release and reaches repos via align-files.
//
// renovate: datasource=orb depName=circleci/continuation
const ContinuationOrbVersion = "2.0.1"

// DefaultAppCatalog and DefaultAppCatalogTest are the catalogs the chart
// pipeline publishes to when a repo does not override them. They match the
// long-standing template hardcodes, so repos that do not set a catalog get the
// identical config they had before the override existed.
const (
	DefaultAppCatalog     = "giantswarm-catalog"
	DefaultAppCatalogTest = "giantswarm-test-catalog"
)

type Config struct {
	// RepoName is the repository name, used for the binary, chart, and job
	// names.
	RepoName string
	// Language is the repo language. "go" selects the go-build job.
	Language gen.Language
	// Flavours are the devctl gen flavours. The "app" flavour selects the
	// chart pipeline.
	Flavours gen.FlavourSlice
	// HasDockerfile selects the image pipeline. The runner derives this from
	// the presence of a Dockerfile in the repo.
	HasDockerfile bool
	// AppCatalog overrides the catalog the chart pipeline publishes to. Empty
	// defaults to "giantswarm-catalog". Set it for repos that ship to a
	// different catalog (e.g. the internal "giantswarm-operations-platform")
	// so generation does not migrate their chart to the public catalog.
	AppCatalog string
	// AppCatalogTest overrides the test catalog. Empty defaults to
	// "giantswarm-test-catalog". Kept paired with AppCatalog.
	AppCatalogTest string
	// ChartName overrides the chart name (the push-to-app-catalog `chart`
	// param and the helm/<chart> directory). Empty defaults to RepoName. Set it
	// for repos whose chart directory does not match the repo name (e.g.
	// docs-proxy ships helm/docs-proxy-app).
	ChartName string
	// ForcePublic pushes the image and chart as public artifacts even though
	// the repo is private (architect force-public: true). Set it for private
	// repos that publish public artifacts (e.g. web-assets). Mutually exclusive
	// with ImagePrivateOnly.
	ForcePublic bool
	// BranchPublish opts the repo into publishing a dev image and chart on
	// branch builds. By default branches build + test only (no push). When
	// set, the branch path additionally pushes an amd64 dev image and the
	// dev chart, coupled (both or neither).
	BranchPublish bool
	// ImagePreBuildJob names a repo-owned custom.yml job the image build must
	// wait on (adds a `requires` entry to push-to-registries-release and the
	// branch build-image / push-to-registries job). Used for workspace-handoff
	// pre-steps the append-only custom.yml merge cannot inject into a generated
	// job. Empty for the common case.
	ImagePreBuildJob string
	// ImageDockerfile overrides the Dockerfile path on the image jobs (the
	// architect push-to-registries `dockerfile` param). A non-empty value also
	// forces the image pipeline on, so a repo whose Dockerfile is not at the
	// repo root (e.g. backstage -> packages/backend/Dockerfile) still generates
	// image jobs. Empty keeps the orb default ("Dockerfile") and leaves the
	// root-Dockerfile derivation untouched.
	ImageDockerfile string
	// ImagePrivateOnly ships the image to the private registry only
	// (gsociprivate), replacing split-china-push and omitting sync-china-registry.
	// Set it for private repos whose image must not land in the public catalog.
	ImagePrivateOnly bool
	// ImageName overrides the `giantswarm/<repo>` default image name on the
	// image jobs. Set it for repos whose published image differs from the repo
	// name (e.g. kserve -> giantswarm/kserve-controller). Empty keeps the orb
	// default.
	ImageName string
	// ImagePlatforms overrides the buildx platform list on the image jobs.
	// Empty lets the orb default apply. Set it for single-architecture images
	// (e.g. vllm -> linux/arm64).
	ImagePlatforms string
}

// shipsBinaries reports whether the repo distributes cross-platform Go binaries
// on its GitHub Release. The "cli" flavour is the signal: it marks a repo whose
// users download a binary, as opposed to a chart-wrapped service or operator.
// Requires Go -- the binary comes from go-build.
func (c Config) shipsBinaries() bool {
	return c.Language == gen.LanguageGo && c.Flavours.Contains(gen.FlavourCLI)
}

type CircleCI struct {
	params params.Params
}

func New(config Config) (*CircleCI, error) {
	// Every job is derived from a signal. With none of them set the template
	// renders an empty `jobs:` list, which is an invalid CircleCI config.
	hasApp := config.Flavours.Contains(gen.FlavourApp)
	// A non-root Dockerfile is signalled by ImageDockerfile: the runner derives
	// HasDockerfile from a root os.Stat that misses it, so the explicit path
	// also turns the image pipeline on.
	hasDockerfile := config.HasDockerfile || config.ImageDockerfile != ""
	if config.Language != gen.LanguageGo && !hasDockerfile && !hasApp {
		return nil, microerror.Maskf(invalidConfigError, "no jobs would be generated: set --language=go, add a Dockerfile, or use the app flavour")
	}

	if config.ForcePublic && config.ImagePrivateOnly {
		return nil, microerror.Maskf(invalidConfigError, "ForcePublic and ImagePrivateOnly are mutually exclusive")
	}

	appCatalog := config.AppCatalog
	if appCatalog == "" {
		appCatalog = DefaultAppCatalog
	}
	appCatalogTest := config.AppCatalogTest
	if appCatalogTest == "" {
		appCatalogTest = DefaultAppCatalogTest
	}

	chartName := config.ChartName
	if chartName == "" {
		chartName = config.RepoName
	}

	c := &CircleCI{
		params: params.Params{
			RepoName:               config.RepoName,
			Language:               config.Language.String(),
			HasDockerfile:          hasDockerfile,
			HasApp:                 hasApp,
			ChartName:              chartName,
			ForcePublic:            config.ForcePublic,
			AppCatalog:             appCatalog,
			AppCatalogTest:         appCatalogTest,
			BranchPublish:          config.BranchPublish,
			ImagePreBuildJob:       config.ImagePreBuildJob,
			ImagePrivateOnly:       config.ImagePrivateOnly,
			ImageName:              config.ImageName,
			ImagePlatforms:         config.ImagePlatforms,
			ImageDockerfile:        config.ImageDockerfile,
			ReleaseBinaries:        config.shipsBinaries(),
			OrbVersion:             OrbVersion,
			ContinuationOrbVersion: ContinuationOrbVersion,
		},
	}

	return c, nil
}

// SetupConfig is the static dynamic-config setup workflow written to
// .circleci/config.yml. It merges the optional repo-owned custom.yml into
// workflows.yml at pipeline runtime.
func (c *CircleCI) SetupConfig() input.Input {
	return file.NewSetupConfigInput(c.params)
}

// Workflows is the derived golden pipeline content written to
// .circleci/workflows.yml.
func (c *CircleCI) Workflows() input.Input {
	return file.NewWorkflowsInput(c.params)
}
