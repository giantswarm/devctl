package circleci

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
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
const OrbVersion = "10.4.0"

// DefaultATSVersion is the app-test-suite container tag the generated chart-test
// jobs run when a repo pins none (`gen circleci --ats-version`). app-test-suite
// 1.x is the default for every generated-CI chart repo: the job creates the kind
// cluster, the tests live in a uv project and the chart is installed with Helm.
// A repo that has not migrated its .ats/main.yaml and tests yet pins a 0.x tag
// (e.g. "0.15.0") to stay on the legacy dats.sh path until it has.
const DefaultATSVersion = "1.0.3"

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

// DefaultNodeImageVersion is the cimg/node Docker tag the generated Node job
// runs on when a repo does not pin its own. Baked in and Renovate-managed, for
// the same reason as the orb pins: a toolchain bump ships with a devctl release
// and reaches repos via align-files rather than drifting per repo.
//
// A repo that needs to own its Node version -- because the version is also
// baked into artifacts devctl does not generate (a Dockerfile FROM, a
// setup-node step) and must not diverge from CI -- overrides this by committing
// a .nvmrc, which the runner probes. That keeps the version in ONE place per
// repo instead of trading a central default for N copies: Renovate's built-in
// nvm manager owns .nvmrc natively (datasource node-version, depName "node"),
// so it groups with the repo's other node deps and is LTS-gated, neither of
// which a rendered cimg/node tag can express.
//
// That trade has one cost worth knowing. This constant is tracked against the
// docker datasource, so "a cimg/node tag with this name exists" is a
// precondition of any bump. A .nvmrc is tracked against node-version -- Node
// releases, which CircleCI trails by hours to days. A repo whose .nvmrc is
// bumped inside that window renders an image that cannot be pulled yet, and
// every job fails at container spin-up with a manifest error until CircleCI
// catches up. It is self-healing and confined to that one repo, but the cause
// is not obvious from the error; a consuming repo that wants the guarantee back
// can hold .nvmrc on the docker datasource in its own renovate config.
//
// renovate: datasource=docker depName=cimg/node
const DefaultNodeImageVersion = "24.20.0"

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
	// SkipATS opts the chart pipeline out of app-test-suite (ATS) chart tests.
	// When set, the run-tests-with-ats jobs and the canonical tests/ats/Pipfile
	// are not generated, and the chart push jobs gate directly on build-chart.
	// Only applies to a chart/app repo (the "app" flavour).
	SkipATS bool
	// ATSOnRelease also runs the app-test-suite (ATS) chart tests on the
	// release tag. By default the chart pipeline runs them once, as
	// execute-chart-tests on every branch build, and the tag only builds and
	// pushes the chart (push-chart-release gates on build-chart): the tag is
	// cut from the merge commit of a PR whose branch run already tested that
	// tree. When set, the pre-v8.45.0 shape is generated: an additional
	// execute-chart-tests-release job on the tag (after the release image when
	// there is one) that push-chart-release gates on. For repos whose custom.yml
	// jobs require execute-chart-tests-release, or whose branch protection does
	// not make the CircleCI statuses required checks so the tag-time run is the
	// only enforced one. Mutually exclusive with SkipATS. Only applies to a
	// chart/app repo.
	ATSOnRelease bool
	// ATSVersion pins the app-test-suite container tag the chart-test jobs run
	// (run-tests-with-ats `app-test-suite_container_tag`). Empty selects
	// DefaultATSVersion. A tag of major 1 or
	// higher selects app-test-suite 1.x, which no longer provisions clusters:
	// both jobs get `create_kind_cluster: true` (the job creates the kind
	// cluster and hands over its kubeconfig) and the generated test dependency
	// file switches from tests/ats/Pipfile (pipenv) to tests/ats/pyproject.toml
	// + uv.lock (uv), with the Pipfile deleted. A 0.x tag keeps the legacy
	// dats.sh path and the Pipfile. Must be a semantic version. Ignored with
	// SkipATS. Only applies to a chart/app repo.
	ATSVersion string
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
	// OverrideChartAppVersion decides whether app-build-suite stamps the
	// computed build version into the chart's appVersion. nil derives it from
	// the repo's shape: a repo that builds its own image ships the app it
	// packages, so its appVersion is its own version and is stamped; a
	// chart-only repo packages an app built elsewhere, so the appVersion
	// declared in Chart.yaml is kept. A non-nil value overrules that in either
	// direction. The append-only custom.yml merge cannot add a param to a
	// generated job, so the generator carries it.
	OverrideChartAppVersion *bool
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
	// ImageNativeBuilds builds the image one architecture per job on a native
	// resource class (architect build-image, one per platform) and switches
	// the push-to-registries jobs to `merge-digests: true`, which joins the
	// per-architecture digests into the tagged index instead of building.
	// False (default) keeps the single multi-platform buildx job. Set it for
	// Dockerfiles with real work in RUN steps (apt, pip, yarn, native
	// modules), where the emulated architecture is the whole critical path; a
	// COPY of a cross-compiled binary gains nothing. Requires architect-orb
	// 10.2.0. Every platform in ImagePlatforms must have a native class.
	ImageNativeBuilds bool
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
	// GoBuildPath overrides the package the go-build job compiles (the
	// architect go-build `path` param). Empty keeps the orb default ".", the
	// module root.
	GoBuildPath string
	// GoTestArtifacts names a directory under the checkout that `make test`
	// writes and that the go-build job keeps as a CircleCI build artifact when
	// it fails: the job gains post-steps that stage the directory when: on_fail
	// and upload the staging directory with store_artifacts, so a green run
	// stores nothing. Empty renders no post-steps. Cleaned with path.Clean; must
	// stay a relative path under the checkout made of [A-Za-z0-9._/-], because
	// the template splices it into a shell command and into the artifact
	// destination verbatim. Requires Language go.
	GoTestArtifacts string
	// PackageManager selects the Node package manager the build/test job uses
	// (one of "npm", "yarn", "yarn-classic", "pnpm"). The runner detects it
	// from the lockfile; empty defaults to Yarn Berry. Only applies to a Node
	// repo (Language == "node").
	PackageManager string
	// NodeImageVersion pins the cimg/node tag the build/test job runs on, and
	// with it the node-build cache-key salt. The runner detects it from the
	// repo's .nvmrc; empty falls back to DefaultNodeImageVersion. Only applies
	// to a Node repo (Language == "node").
	NodeImageVersion string
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

// DefaultImagePlatforms is the platform list a native per-architecture image
// build covers when the repo does not override it. It matches the orb's own
// default for the single-job build, so opting in does not change the set.
const DefaultImagePlatforms = "linux/amd64,linux/arm64"

// imageResourceClasses maps a build platform to a CircleCI resource class of the
// same architecture. This mapping is the whole point of building one
// architecture per job: CircleCI gives the setup_remote_docker VM the
// architecture of the job's resource class, so a class from this table is what
// keeps the build native. The orb refuses a mismatch rather than falling back to
// QEMU, so a wrong entry here is a failed build, not a slow one.
//
// amd64 sits on `small` rather than `medium` because a small docker executor is
// already given a medium remote-docker VM, which is where the build runs.
// arm.medium is the floor on the Arm side; there is no arm.small.
var imageResourceClasses = map[string]string{
	"linux/amd64": "small",
	"linux/arm64": "arm.medium",
}

// resolveImageBuilds turns a comma-separated platform list into one build-image
// job per platform, for each of the branch and tag paths, and returns the
// normalised list to put on the push-to-registries merge jobs.
//
// Branch and tag jobs need distinct names because both appear in the same
// workflow. An unmapped platform is an error rather than a default class: the
// orb has no emulated fallback, so guessing a class here would generate a
// pipeline that fails at build time instead of at generation time.
func resolveImageBuilds(platforms string) (string, []params.ImageBuild, []params.ImageBuild, error) {
	if platforms == "" {
		platforms = DefaultImagePlatforms
	}

	var resolved []string
	var branch, release []params.ImageBuild
	for _, platform := range strings.Split(platforms, ",") {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}

		resourceClass, ok := imageResourceClasses[platform]
		if !ok {
			supported := make([]string, 0, len(imageResourceClasses))
			for p := range imageResourceClasses {
				supported = append(supported, p)
			}
			sort.Strings(supported)

			return "", nil, nil, microerror.Maskf(invalidConfigError,
				"ImageNativeBuilds: no CircleCI resource class is mapped for platform %#q; supported platforms are %s",
				platform, strings.Join(supported, ", "))
		}

		suffix := strings.ReplaceAll(strings.TrimPrefix(platform, "linux/"), "/", "")
		resolved = append(resolved, platform)
		branch = append(branch, params.ImageBuild{
			Name:          "build-image-" + suffix,
			Platform:      platform,
			ResourceClass: resourceClass,
		})
		release = append(release, params.ImageBuild{
			Name:          "build-image-release-" + suffix,
			Platform:      platform,
			ResourceClass: resourceClass,
		})
	}

	if len(resolved) == 0 {
		return "", nil, nil, microerror.Maskf(invalidConfigError, "ImageNativeBuilds: ImagePlatforms resolved to an empty platform list")
	}

	return strings.Join(resolved, ","), branch, release, nil
}

// goTestArtifactsPattern bounds what GoTestArtifacts may contain: the template
// splices the value into a shell command (cp) and into the store_artifacts
// destination without quoting, so anything beyond a plain path is rejected at
// generation time rather than interpreted by the CI shell.
var goTestArtifactsPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func New(config Config) (*CircleCI, error) {
	// Every job is derived from a signal. With none of them set the template
	// renders an empty `jobs:` list, which is an invalid CircleCI config.
	hasApp := config.Flavours.Contains(gen.FlavourApp)
	// A non-root Dockerfile is signalled by ImageDockerfile: the runner derives
	// HasDockerfile from a root os.Stat that misses it, so the explicit path
	// also turns the image pipeline on.
	hasDockerfile := config.HasDockerfile || config.ImageDockerfile != ""
	// appVersion is the version of the packaged application. A repo that builds
	// its own image ships the app it packages, so appVersion is its own version
	// and app-build-suite stamps it. A chart-only repo packages an app built
	// elsewhere, so the appVersion declared in Chart.yaml has to survive
	// packaging. An explicit OverrideChartAppVersion overrules the derivation.
	keepChartAppVersion := !hasDockerfile
	if config.OverrideChartAppVersion != nil {
		keepChartAppVersion = !*config.OverrideChartAppVersion
	}
	isNode := config.Language == gen.LanguageNode
	if config.Language != gen.LanguageGo && !isNode && !hasDockerfile && !hasApp {
		return nil, microerror.Maskf(invalidConfigError, "no jobs would be generated: set --language=go or --language=node, add a Dockerfile, or use the app flavour")
	}

	if config.ForcePublic && config.ImagePrivateOnly {
		return nil, microerror.Maskf(invalidConfigError, "ForcePublic and ImagePrivateOnly are mutually exclusive")
	}
	if config.SkipATS && config.ATSOnRelease {
		return nil, microerror.Maskf(invalidConfigError, "SkipATS and ATSOnRelease are mutually exclusive")
	}
	// SkipATS drops the chart-test jobs and the test dependency files, so the
	// ATS tag is moot; it is not an error to carry the default alongside it.
	if config.ATSVersion == "" {
		config.ATSVersion = DefaultATSVersion
	}
	atsKindCluster, err := atsCreatesKindCluster(config.ATSVersion)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	// The single-job build passes ImagePlatforms through untouched (empty lets
	// the orb derive it). The native per-architecture build has to know the
	// set at generation time, because it emits one job per platform.
	imagePlatforms := config.ImagePlatforms
	var branchImageBuilds, releaseImageBuilds []params.ImageBuild
	if config.ImageNativeBuilds {
		var err error
		imagePlatforms, branchImageBuilds, releaseImageBuilds, err = resolveImageBuilds(config.ImagePlatforms)
		if err != nil {
			return nil, microerror.Mask(err)
		}
	}

	if config.GoBuildPath != "" && config.Language != gen.LanguageGo {
		return nil, microerror.Maskf(invalidConfigError, "GoBuildPath requires Language go")
	}

	goTestArtifacts := ""
	if config.GoTestArtifacts != "" {
		if config.Language != gen.LanguageGo {
			return nil, microerror.Maskf(invalidConfigError, "GoTestArtifacts requires Language go")
		}
		goTestArtifacts = path.Clean(config.GoTestArtifacts)
		if path.IsAbs(goTestArtifacts) || goTestArtifacts == "." || goTestArtifacts == ".." || strings.HasPrefix(goTestArtifacts, "../") || !goTestArtifactsPattern.MatchString(goTestArtifacts) {
			return nil, microerror.Maskf(invalidConfigError, "GoTestArtifacts must be a relative directory under the checkout made of [A-Za-z0-9._/-], got %q", config.GoTestArtifacts)
		}
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
		nodeImageVersion         string
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
		// The repo's own pin (.nvmrc, detected by the runner) wins over the
		// baked-in default, so a repo that must keep CI in step with a Node
		// version it also bakes elsewhere has one place to change.
		nodeImageVersion = config.NodeImageVersion
		if nodeImageVersion == "" {
			nodeImageVersion = DefaultNodeImageVersion
		}
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
			nodeBuildCacheRestoreKey = "node-build-" + pm + "-v1-" + nodeImageVersion + "-"
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
			SkipATS:                  config.SkipATS,
			ATSVersion:               config.ATSVersion,
			ATSKindCluster:           atsKindCluster,
			ATSOnRelease:             config.ATSOnRelease,
			ChartName:                chartName,
			KeepChartAppVersion:      keepChartAppVersion,
			ForcePublic:              config.ForcePublic,
			AppCatalog:               appCatalog,
			AppCatalogTest:           appCatalogTest,
			BranchPublish:            config.BranchPublish,
			ImagePreBuildJob:         config.ImagePreBuildJob,
			ImagePrivateOnly:         config.ImagePrivateOnly,
			ImageName:                config.ImageName,
			ImagePlatforms:           imagePlatforms,
			ImageNativeBuilds:        config.ImageNativeBuilds,
			BranchImageBuilds:        branchImageBuilds,
			ReleaseImageBuilds:       releaseImageBuilds,
			ImageDockerfile:          config.ImageDockerfile,
			ReleaseBinaries:          config.shipsBinaries(),
			BuildConcurrency:         buildConcurrency,
			ResourceClass:            resourceClass,
			GoBuildPath:              config.GoBuildPath,
			GoTestArtifacts:          goTestArtifacts,
			OrbVersion:               OrbVersion,
			ContinuationOrbVersion:   ContinuationOrbVersion,
			BuildJobName:             buildJobName,
			NodeJobName:              nodeJobName,
			NodeImageVersion:         nodeImageVersion,
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
// dependent on a separate, differently-scoped invocation. A repo that opts out
// of ATS (SkipATS) gets no Pipfile either, matching the suppressed jobs. The
// default branch-only shape keeps the file (the branch job runs the tests);
// ATSOnRelease only adds the tag-time job.
func (c *CircleCI) ATSInputs() []input.Input {
	if !c.params.HasApp || c.params.SkipATS {
		return nil
	}

	return ats.CreateATS(c.params.ATSKindCluster)
}

// atsCreatesKindCluster decides from the app-test-suite container tag whether
// the chart-test jobs must create the kind cluster themselves: app-test-suite
// 1.0 dropped its built-in kind lifecycle, so every 1.x tag needs
// `create_kind_cluster: true` (and the uv test layout), while 0.x tags keep the
// legacy dats.sh path. New substitutes DefaultATSVersion for an empty tag before
// this runs; an empty tag here still means "no opinion". The tag has to parse
// as a semantic version (an optional leading "v" is tolerated); a dev tag such
// as 0.15.1-dev.<branch>.<date>.<hash> counts as 0.x.
func atsCreatesKindCluster(tag string) (bool, error) {
	if tag == "" {
		return false, nil
	}
	v, err := semver.NewVersion(strings.TrimPrefix(tag, "v"))
	if err != nil {
		return false, microerror.Maskf(invalidConfigError, "ATSVersion %q is not a semantic version: %v", tag, err)
	}
	return v.Major() >= 1, nil
}
