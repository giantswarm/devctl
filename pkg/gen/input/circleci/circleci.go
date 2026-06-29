package circleci

import (
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/ats"
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
// Tracked via github-tags on the architect-orb source repo rather than the
// `orb` datasource: the generated renovate.json5 disables `orb` updates for
// giantswarm/architect (so they stop fighting align-files in .circleci/config.yml),
// and that root packageRule would otherwise also block this constant. The
// custom manager that reads this annotation lives in renovate-custom.json5.
//
// renovate: datasource=github-tags depName=giantswarm/architect-orb
const OrbVersion = "9.5.5"

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

// NodeImageVersion is the cimg/node Docker tag the generated Node job runs on.
// Baked in (not a flag) and Renovate-managed, for the same reason as the orb
// pins: a toolchain bump ships with a devctl release and reaches repos via
// align-files rather than drifting per repo.
//
// renovate: datasource=docker depName=cimg/node
const NodeImageVersion = "24.18.0"

// DefaultNodeTestTarget is the package.json script the Node job runs for the
// verify phase when a repo does not override it. The repo composes
// typecheck/lint/format/test into its own ci:verify script -- the make-target
// interface (the Node analogue of `make test`), so CI and local runs share one
// command.
//
// `test` is only a FLOOR, not the convention: a bare `test` script yields a
// green job but gates tests alone, silently skipping typecheck/lint/format. The
// convention is an explicit composed ci:verify (tsc --noEmit + lint + prettier
// --check + tests, in one process; backstage is the reference), set via
// gen.ci.node.testTarget. ci:verify owns lint/format CI-wide (verify-canonical:
// the CI pre-commit job carries no JS/TS hook), and the companion ci:build
// (NodeBuildTarget) is bundle/emit-only so nothing runs twice -- the single-pass
// contract recorded in the node-ci-verify-build-single-pass ADR.
const DefaultNodeTestTarget = "test"

// DefaultNodeResourceClass is the CircleCI resource_class the Node job runs on
// when a repo does not override it. The Node verify chain (tsc + lint + test +
// build over a whole monorepo) is memory-hungry -- backstage's ci:verify pins
// NODE_OPTIONS max-old-space-size to 6 GiB -- so "large" (4 vCPU / 8 GiB) is the
// floor. A bigger monorepo raises it via gen.ci.resourceClass, the same knob the
// cli go-build job uses.
const DefaultNodeResourceClass = "large"

// Package-manager values detected from the lockfile. Yarn Berry and Yarn
// Classic are distinguished because their install commands and cache
// directories differ (Berry: `--immutable` + .yarn/cache; Classic:
// `--frozen-lockfile` + ~/.cache/yarn), and the two cannot be told apart from
// the lockfile name alone.
const (
	PackageManagerNPM         = "npm"
	PackageManagerYarn        = "yarn"
	PackageManagerYarnClassic = "yarn-classic"
	PackageManagerPNPM        = "pnpm"
)

// Lockfile names the cache is keyed on, per package manager.
const (
	lockfileNPM  = "package-lock.json"
	lockfileYarn = "yarn.lock"
	lockfilePNPM = "pnpm-lock.yaml"
)

// nodeToolchain is the per-package-manager install command, cache location,
// and script-run prefix the Node job renders. Detection of which package
// manager a repo uses happens in the runner (from the lockfile); this maps the
// detected manager to its concrete commands.
type nodeToolchain struct {
	installCommand string
	runPrefix      string
	cachePath      string
	lockfile       string
	corepack       bool
	// buildCachePaths is the build-output cache: the materialized dependency
	// tree (and Yarn's install-state) that holds compiled native addons. The
	// dependency cache (cachePath) only holds package *tarballs*; the expensive
	// part of `install` on a node-modules-linker repo is the Link step
	// (unpacking + node-gyp builds of better-sqlite3, isolated-vm, etc.), whose
	// output lives in node_modules, not the tarball cache. Restoring it lets the
	// install reconcile incrementally instead of recompiling from source every
	// run -- the Node analogue of go-build persisting $GOCACHE. Keyed on the
	// node image version too (native ABI is node-version-specific). The template
	// saves it after the verify/build steps, so the same cache also persists the
	// tsc/eslint/jest incremental caches those tools write under
	// node_modules/.cache (the compute-side win). Empty for package managers
	// where it does not apply: npm (`npm ci` wipes node_modules first) and pnpm
	// (its content-addressable store already caches build side-effects, and that
	// store is the dependency cache).
	buildCachePaths []string
}

func nodeToolchainFor(packageManager string) nodeToolchain {
	switch packageManager {
	case PackageManagerNPM:
		return nodeToolchain{
			installCommand: "npm ci",
			runPrefix:      "npm run",
			cachePath:      "~/.npm",
			lockfile:       lockfileNPM,
		}
	case PackageManagerPNPM:
		// pnpm is not bundled with cimg/node, so it is activated via corepack.
		// ponytail: the cache assumes pnpm's default store location; a repo
		// with a custom store-dir would need the path threaded through.
		return nodeToolchain{
			installCommand: "pnpm install --frozen-lockfile",
			runPrefix:      "pnpm run",
			cachePath:      "~/.local/share/pnpm/store",
			lockfile:       lockfilePNPM,
			corepack:       true,
		}
	case PackageManagerYarnClassic:
		return nodeToolchain{
			installCommand:  "yarn install --frozen-lockfile",
			runPrefix:       "yarn run",
			cachePath:       "~/.cache/yarn",
			lockfile:        lockfileYarn,
			buildCachePaths: []string{"node_modules"},
		}
	default: // PackageManagerYarn (Berry) is the default for an unset value.
		return nodeToolchain{
			installCommand: "yarn install --immutable",
			runPrefix:      "yarn run",
			cachePath:      ".yarn/cache",
			lockfile:       lockfileYarn,
			// .yarn/install-state.gz records which packages have been built, so
			// restoring it alongside node_modules lets `yarn install
			// --immutable` skip the native rebuild instead of redoing it.
			buildCachePaths: []string{"node_modules", ".yarn/install-state.gz"},
		}
	}
}

// DefaultBuildConcurrency and DefaultResourceClass are the go-build knobs the
// cli flavour applies when a repo does not override them. They match the
// long-standing template hardcodes, so cli repos that set neither render the
// identical config they had before the overrides existed. Only the cli flavour
// (ReleaseBinaries) emits these; a non-cli go-build job stays on the orb/CircleCI
// defaults.
const (
	DefaultBuildConcurrency = "auto"
	DefaultResourceClass    = "large"
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
	// BuildConcurrency overrides how many architectures the cli-flavour
	// go-build job compiles concurrently (the architect go-build
	// `build_concurrency` param). Empty defaults to "auto" (nproc). Lower it
	// (e.g. "2") for repos whose binary is large enough that a cold full-matrix
	// cross-compile OOMs the runner at `auto` -- memory, not CPU, is the binding
	// constraint, and a killed build never stores the build cache, so the repo
	// stays permanently cold. Only applies to the cli flavour (ReleaseBinaries).
	BuildConcurrency string
	// ResourceClass overrides the CircleCI resource_class on the cli-flavour
	// go-build job. Empty defaults to "large". Raise it (e.g. "xlarge") for
	// repos that need more RAM/CPU headroom for the cold cross-compile. Only
	// applies to the cli flavour (ReleaseBinaries).
	ResourceClass string
	// PackageManager selects the Node package manager the build/test job uses
	// (one of "npm", "yarn", "yarn-classic", "pnpm"). The runner detects it
	// from the lockfile; empty defaults to Yarn Berry. Only applies to a Node
	// repo (Language == "node").
	PackageManager string
	// NodeTestTarget overrides the package.json script the Node job runs for
	// the verify phase (ci:verify). Empty defaults to "test". The repo composes
	// its entire correctness gate -- tsc --noEmit + lint + prettier --check +
	// unit tests, in one process -- into this one script (the make-target
	// interface). The default "test" is only a floor; the convention is an
	// explicit composed ci:verify. Only applies to a Node repo.
	NodeTestTarget string
	// NodeBuildTarget is the package.json script the Node job runs to build
	// (ci:build). Empty omits the build step (a library that only verifies).
	// It must be bundle/emit-only -- it must redo nothing NodeTestTarget already
	// did (no second typecheck/lint/test) and must not re-install. Only applies
	// to a Node repo.
	NodeBuildTarget string
	// NodeBuildOutput is the workspace path the Node job persists for an image
	// handoff (e.g. backstage's "packages/*/dist/*"). Non-empty names the job
	// "node-build" and emits persist_to_workspace; empty names it "node-test".
	// Only applies to a Node repo.
	NodeBuildOutput string
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
	isNode := config.Language == gen.LanguageNode
	if config.Language != gen.LanguageGo && !isNode && !hasDockerfile && !hasApp {
		return nil, microerror.Maskf(invalidConfigError, "no jobs would be generated: set --language=go or --language=node, add a Dockerfile, or use the app flavour")
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

	// Node toolchain. The build/test job is self-contained on a cimg/node
	// executor (not an architect orb job -- the orb ships none), defined inline
	// in workflows.yml. Its name signals what it produces: node-build when the
	// build output is persisted for an image handoff, node-test otherwise.
	var (
		nodeJobName              string
		nodeInstallCommand       string
		nodeRunPrefix            string
		nodeCachePath            string
		nodeCacheKey             string
		nodeCacheRestoreKey      string
		nodeBuildCachePaths      []string
		nodeBuildCacheKey        string
		nodeBuildCacheRestoreKey string
		nodeCorepack             bool
		nodeResourceClass        string
		nodeTestTarget           string
		nodeBuildTarget          string
		nodeBuildOutput          string
	)
	if isNode {
		tc := nodeToolchainFor(config.PackageManager)
		nodeInstallCommand = tc.installCommand
		nodeRunPrefix = tc.runPrefix
		nodeCachePath = tc.cachePath
		nodeBuildCachePaths = tc.buildCachePaths
		nodeCorepack = tc.corepack
		// The cli go-build resourceClass knob (gen.ci.resourceClass) is shared:
		// a Node repo reuses it to size the verify/build box, defaulting to
		// "large" when unset.
		nodeResourceClass = config.ResourceClass
		if nodeResourceClass == "" {
			nodeResourceClass = DefaultNodeResourceClass
		}
		// Embed the literal CircleCI `{{ checksum }}` expression as a plain Go
		// string so it survives Go-template rendering untouched and is
		// evaluated by CircleCI at pipeline time. Key on the package manager so
		// switching managers cannot collide cache entries.
		pm := config.PackageManager
		if pm == "" {
			pm = PackageManagerYarn
		}
		// `v1` is a cache-version salt. CircleCI cache keys are immutable, so a
		// repo that first adopts the Node job while still on Yarn's default
		// global cache seeds an empty .yarn/cache under the lockfile hash; the
		// real cache can then never be saved until the lockfile changes. Bumping
		// the salt invalidates such stale/empty seeds in one release and gives a
		// lever to invalidate caches on future cache-shape changes.
		nodeCacheRestoreKey = "node-deps-" + pm + "-v1-"
		nodeCacheKey = nodeCacheRestoreKey + `{{ checksum "` + tc.lockfile + `" }}`

		// Build-output cache (yarn only -- see nodeToolchain.buildCachePaths).
		// Keyed on the node image version as well as the lockfile because the
		// cached node_modules holds compiled native addons whose ABI is tied to
		// the node version, so a node bump must not restore stale binaries. The
		// restore prefix omits the lockfile checksum, so a changed lockfile
		// still warm-starts from the previous node_modules and the install only
		// reconciles (and rebuilds) the diff. The template saves this cache
		// *after* the verify/build steps, so it captures the tsc/eslint/jest
		// incremental caches those tools write under node_modules/.cache too --
		// the compute-side analogue of go-build persisting $GOCACHE.
		if len(nodeBuildCachePaths) > 0 {
			nodeBuildCacheRestoreKey = "node-build-" + pm + "-v1-" + NodeImageVersion + "-"
			nodeBuildCacheKey = nodeBuildCacheRestoreKey + `{{ checksum "` + tc.lockfile + `" }}`
		}

		nodeTestTarget = config.NodeTestTarget
		if nodeTestTarget == "" {
			nodeTestTarget = DefaultNodeTestTarget
		}
		nodeBuildTarget = config.NodeBuildTarget
		nodeBuildOutput = config.NodeBuildOutput
		if nodeBuildOutput != "" {
			nodeJobName = "node-build"
		} else {
			nodeJobName = "node-test"
		}
	}

	// BuildJobName unifies the language-derived `requires` wiring: the image
	// and chart jobs gate on whichever build/test job the language emits.
	buildJobName := ""
	switch config.Language {
	case gen.LanguageGo:
		buildJobName = "go-build"
	case gen.LanguageNode:
		buildJobName = nodeJobName
	}

	// The cli flavour emits build_concurrency + resource_class; default the
	// pair to the long-standing template hardcodes when unset so existing cli
	// repos are unchanged. Non-cli repos leave both empty -- the template only
	// renders them inside the ReleaseBinaries block.
	buildConcurrency := config.BuildConcurrency
	resourceClass := config.ResourceClass
	if config.shipsBinaries() {
		if buildConcurrency == "" {
			buildConcurrency = DefaultBuildConcurrency
		}
		if resourceClass == "" {
			resourceClass = DefaultResourceClass
		}
	}

	c := &CircleCI{
		params: params.Params{
			RepoName:                 config.RepoName,
			Language:                 config.Language.String(),
			HasDockerfile:            hasDockerfile,
			HasApp:                   hasApp,
			ChartName:                chartName,
			ForcePublic:              config.ForcePublic,
			AppCatalog:               appCatalog,
			AppCatalogTest:           appCatalogTest,
			BranchPublish:            config.BranchPublish,
			ImagePreBuildJob:         config.ImagePreBuildJob,
			ImagePrivateOnly:         config.ImagePrivateOnly,
			ImageName:                config.ImageName,
			ImagePlatforms:           config.ImagePlatforms,
			ImageDockerfile:          config.ImageDockerfile,
			ReleaseBinaries:          config.shipsBinaries(),
			BuildConcurrency:         buildConcurrency,
			ResourceClass:            resourceClass,
			OrbVersion:               OrbVersion,
			ContinuationOrbVersion:   ContinuationOrbVersion,
			BuildJobName:             buildJobName,
			NodeJobName:              nodeJobName,
			NodeImageVersion:         NodeImageVersion,
			NodeInstallCommand:       nodeInstallCommand,
			NodeRunPrefix:            nodeRunPrefix,
			NodeCachePath:            nodeCachePath,
			NodeCacheKey:             nodeCacheKey,
			NodeCacheRestoreKey:      nodeCacheRestoreKey,
			NodeBuildCachePaths:      nodeBuildCachePaths,
			NodeBuildCacheKey:        nodeBuildCacheKey,
			NodeBuildCacheRestoreKey: nodeBuildCacheRestoreKey,
			NodeCorepack:             nodeCorepack,
			NodeResourceClass:        nodeResourceClass,
			NodeTestTarget:           nodeTestTarget,
			NodeBuildTarget:          nodeBuildTarget,
			NodeBuildOutput:          nodeBuildOutput,
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

// ATSInputs returns the canonical app-test-suite (ATS) Pipfile input for
// chart/app (.HasApp) repos, and nil otherwise. ATS chart tests run only for
// .HasApp -- the same signal that gates the run-tests-with-ats jobs -- so the
// Pipfile is emitted under exactly that condition and from the same generator
// call site (devctl gen circleci, the only generator invoked inside align's
// `if (ci && ci.generate)` guard). That makes "ATS Pipfile only when CI is
// generated, and only for chart/app repos" structurally guaranteed rather than
// dependent on a separate, differently-scoped invocation.
func (c *CircleCI) ATSInputs() []input.Input {
	if !c.params.HasApp {
		return nil
	}

	return ats.CreateATS()
}
