package circleci

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	flagAppCatalog              = "app-catalog"
	flagAppCatalogTest          = "app-catalog-test"
	flagBranchPublish           = "branch-publish"
	flagBuildConcurrency        = "build-concurrency"
	flagChartName               = "chart-name"
	flagKeepChartAppVersion     = "keep-chart-app-version"
	flagOverrideChartAppVersion = "override-chart-app-version"
	flagForcePublic             = "force-public"
	flagImagePreBuildJob        = "image-pre-build-job"
	flagImagePrivateOnly        = "image-private-only"
	flagImageName               = "image-name"
	flagImagePlatforms          = "image-platforms"
	flagImageDockerfile         = "image-dockerfile"
	flagImageNativeBuilds       = "image-native-builds"
	flagResourceClass           = "resource-class"
	flagSkipATS                 = "skip-ats"
	flagATSBranchOnly           = "ats-branch-only"
	flagATSVersion              = "ats-version"
	flagFlavour                 = "flavour"
	flagLanguage                = "language"
	flagRepoName                = "repo-name"
	flagPackageManager          = "package-manager"
	flagNodeImageVersion        = "node-image-version"
	flagNodeTestTarget          = "node-test-target"
	flagNodeBuildTarget         = "node-build-target"
	flagNodeBuildOutput         = "node-build-output"
)

type flag struct {
	AppCatalog              string
	AppCatalogTest          string
	BranchPublish           bool
	BuildConcurrency        string
	ChartName               string
	KeepChartAppVersion     bool
	OverrideChartAppVersion bool
	ForcePublic             bool
	ImagePreBuildJob        string
	ImagePrivateOnly        bool
	ImageName               string
	ImagePlatforms          string
	ImageDockerfile         string
	ImageNativeBuilds       bool
	ResourceClass           string
	SkipATS                 bool
	ATSBranchOnly           bool
	ATSVersion              string
	Flavours                gen.FlavourSlice
	Language                gen.Language
	RepoName                string
	PackageManager          string
	NodeImageVersion        string
	NodeTestTarget          string
	NodeBuildTarget         string
	NodeBuildOutput         string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.AppCatalog, flagAppCatalog, "", `Catalog the chart pipeline publishes to (push-to-app-catalog app_catalog). Empty defaults to "giantswarm-catalog"; set it for repos that ship to a different catalog (e.g. the internal "giantswarm-operations-platform") so generation does not migrate the chart to the public catalog.`)
	cmd.Flags().StringVar(&f.AppCatalogTest, flagAppCatalogTest, "", `Test catalog the chart pipeline publishes to (push-to-app-catalog app_catalog_test). Empty defaults to "giantswarm-test-catalog". Kept paired with --app-catalog.`)
	cmd.Flags().BoolVar(&f.BranchPublish, flagBranchPublish, false, "Publish a dev image and chart on branch builds. By default branches build + test only (no push); when set, the branch path additionally pushes an amd64 dev image and the dev chart (coupled).")
	cmd.Flags().StringVar(&f.BuildConcurrency, flagBuildConcurrency, "", `Override how many architectures the cli-flavour go-build job compiles concurrently (architect go-build "build_concurrency" param). Empty defaults to "auto" (nproc). Lower it (e.g. "2") for repos whose binary is large enough that a cold full-matrix cross-compile OOMs the runner at "auto" -- memory, not CPU, is the binding constraint, and a killed build never stores the build cache. Only applies to the cli flavour.`)
	cmd.Flags().StringVar(&f.ChartName, flagChartName, "", "Override the chart name (the push-to-app-catalog `chart` param and the helm/<chart> directory). Empty defaults to the repo name. Set it for repos whose chart directory does not match the repo name (e.g. docs-proxy -> docs-proxy-app). The append-only custom.yml merge cannot rename a generated job's chart.")
	cmd.Flags().BoolVar(&f.OverrideChartAppVersion, flagOverrideChartAppVersion, true, "Whether app-build-suite stamps the computed build version into the chart's appVersion. Leave it UNSET to derive it from the repo: a repo that builds its own image ships the app it packages, so its appVersion is its own version and gets stamped; a chart-only repo packages an app built elsewhere, so the appVersion declared in Chart.yaml is kept. Pass it explicitly only to overrule that: `=false` for a repo that builds an image and still declares a foreign appVersion, `=true` for a chart-only repo that wants its appVersion stamped anyway. The chart version is always stamped either way.")
	cmd.Flags().BoolVar(&f.KeepChartAppVersion, flagKeepChartAppVersion, false, "Deprecated alias for --override-chart-app-version=false.")
	_ = cmd.Flags().MarkDeprecated(flagKeepChartAppVersion, fmt.Sprintf("use --%s=false", flagOverrideChartAppVersion))
	cmd.Flags().BoolVar(&f.ForcePublic, flagForcePublic, false, "Push the image and chart as public artifacts even though the repo is private (architect `force-public: true`). Set it for private repos that publish public artifacts (e.g. web-assets). Mutually exclusive with --image-private-only. The append-only custom.yml merge cannot add this to a generated job.")
	cmd.Flags().StringVar(&f.ImagePreBuildJob, flagImagePreBuildJob, "", "Name of a repo-owned job (defined in .circleci/custom.yml) the release image build must wait on. Adds a `requires` entry to push-to-registries-release, which the append-only custom.yml merge cannot inject into a generated job. Used for workspace-handoff pre-steps. Empty for the common case.")
	cmd.Flags().BoolVar(&f.ImagePrivateOnly, flagImagePrivateOnly, false, "Ship the image to the private registry only (gsociprivate), replacing split-china-push and omitting the sync-china-registry job. Set it for private repos whose image must not land in the public catalog.")
	cmd.Flags().StringVar(&f.ImageName, flagImageName, "", "Override the `giantswarm/<repo>` default image name on the image jobs (push-to-registries / sync-china-registry `image` param). Set it for repos whose published image differs from the repo name (e.g. kserve -> giantswarm/kserve-controller). The append-only custom.yml merge cannot rename a generated job's image. Empty keeps the orb default.")
	cmd.Flags().StringVar(&f.ImagePlatforms, flagImagePlatforms, "", "Override the buildx platform list on the image jobs (push-to-registries `platforms` param). Empty lets the orb default apply (linux/amd64,linux/arm64 when no go-build .platforms file). Set it for single-architecture images (e.g. vllm -> linux/arm64, whose amd64 build has no prebuilt wheels).")
	cmd.Flags().StringVar(&f.ImageDockerfile, flagImageDockerfile, "", "Override the Dockerfile path on the image jobs (push-to-registries `dockerfile` param). Set it for repos whose Dockerfile is not at the repo root (e.g. backstage -> packages/backend/Dockerfile); a non-empty value also turns the image pipeline on, since the root-Dockerfile derivation misses a nested Dockerfile. The append-only custom.yml merge cannot set this on a generated job. Empty keeps the orb default.")
	cmd.Flags().BoolVar(&f.ImageNativeBuilds, flagImageNativeBuilds, false, "Build the image one architecture per job on a native resource class instead of one multi-platform buildx job. Emits an architect/build-image job per entry in --image-platforms (linux/amd64 on small, linux/arm64 on arm.medium) and switches the generated push-to-registries jobs to merge-digests: true, which joins the per-architecture digests into the tagged index. Nothing is emulated and the builds run concurrently, so wall clock is the slower single native build; pays off for Dockerfiles with real work in RUN steps (apt, pip, yarn, native modules), not for a COPY of a cross-compiled binary. A platform with no native class is rejected at generation time. On the branch path the validate-only build-image job becomes build-image-<arch> jobs, so a custom.yml that requires build-image must follow. Requires architect-orb 10.2.0.")
	cmd.Flags().StringVar(&f.ResourceClass, flagResourceClass, "", `Override the CircleCI resource_class on the cli-flavour go-build job. Empty defaults to "large". Raise it (e.g. "xlarge") for repos that need more RAM/CPU headroom for the cold cross-compile. Only applies to the cli flavour.`)
	cmd.Flags().BoolVar(&f.SkipATS, flagSkipATS, false, `Opt the chart pipeline out of app-test-suite (ATS) chart tests. By default an "app" flavour repo runs architect/run-tests-with-ats between build-chart and the chart push, and generation emits the canonical tests/ats/Pipfile. When set, those test jobs and the Pipfile are not generated and the chart push gates directly on build-chart. Only applies to the app flavour.`)
	cmd.Flags().BoolVar(&f.ATSBranchOnly, flagATSBranchOnly, false, `Run the app-test-suite (ATS) chart tests on branches only and not again on the release tag. By default the chart pipeline runs architect/run-tests-with-ats twice: execute-chart-tests on every branch build and execute-chart-tests-release on the tag, between build-chart and push-chart-release. When set, execute-chart-tests-release is not generated and push-chart-release gates directly on build-chart, so the release is the build + push alone. The tests/ats/Pipfile is still generated. The trade: the tag is cut from the merge commit of a PR whose execute-chart-tests already passed on that tree, so the tag-time run re-tests an identical tree -- but only if the repo's branch protection makes the CircleCI statuses required checks on the default branch and requires branches to be up to date with it (strict) before merging. Set that first. Mutually exclusive with --skip-ats. Only applies to the app flavour.`)
	cmd.Flags().StringVar(&f.ATSVersion, flagATSVersion, "", `app-test-suite container tag the generated chart-test jobs run (run-tests-with-ats app-test-suite_container_tag). Empty keeps the orb default. A 1.x tag also sets create_kind_cluster: true on both jobs -- app-test-suite 1.x no longer provisions clusters, the job creates the kind cluster and hands over its kubeconfig -- and switches the generated test dependency file from tests/ats/Pipfile (pipenv, ATS <= 0.15) to tests/ats/pyproject.toml + uv.lock (uv, ATS 1.x), deleting the Pipfile. The repo migrates the rest itself (.ats/main.yaml without the *-cluster-type keys, tests that install with Helm instead of an App CR). Mutually exclusive with --skip-ats. Only applies to the app flavour.`)
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`List of project flavours. The "app" flavour selects the chart pipeline. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().VarP(gen.NewLanguageFlagValue(&f.Language, gen.Language("")), flagLanguage, "l", fmt.Sprintf(`The programming language. "go" selects the go-build job. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
	cmd.Flags().StringVarP(&f.RepoName, flagRepoName, "r", "", "Repository name under the giantswarm organization (used for the binary, chart, and job names).")
	cmd.Flags().StringVar(&f.PackageManager, flagPackageManager, "", `Node package manager for the build/test job (one of "npm", "yarn", "yarn-classic", "pnpm"). Empty detects it from the lockfile (package-lock.json -> npm, pnpm-lock.yaml -> pnpm, yarn.lock -> yarn Berry or yarn-classic by its header). Only applies with --language=node.`)
	cmd.Flags().StringVar(&f.NodeImageVersion, flagNodeImageVersion, "", `cimg/node tag the build/test job runs on, which also salts the node-build cache key. Empty detects it from the repo's .nvmrc, and falls back to devctl's baked-in default when there is none. Committing a .nvmrc is how a repo keeps CI in step with a Node version it also bakes into artifacts devctl does not generate (a Dockerfile FROM, a setup-node step) from one place. Only an exact major.minor.patch is read from .nvmrc -- aliases ("lts/*") and less specific versions are ignored with a warning, because a floating tag would drift from the exact patch the repo's Dockerfile pins and would coarsen the cache-key salt. Only applies with --language=node.`)
	cmd.Flags().StringVar(&f.NodeTestTarget, flagNodeTestTarget, "", `package.json script the Node job runs for the verify phase, ci:verify (the make-target interface; the repo composes its whole correctness gate -- tsc --noEmit + lint + prettier --check + tests, in one process -- into it). Empty defaults to "test", which is only a floor: the convention is an explicit composed ci:verify (lint/format live here CI-wide). Only applies with --language=node.`)
	cmd.Flags().StringVar(&f.NodeBuildTarget, flagNodeBuildTarget, "", "package.json script the Node job runs to build, ci:build. Empty omits the build step (a library that only verifies). Must be bundle/emit-only -- redo nothing the verify script did (no second typecheck/lint/test) and no re-install. Only applies with --language=node.")
	cmd.Flags().StringVar(&f.NodeBuildOutput, flagNodeBuildOutput, "", `Workspace path the Node job persists for an image handoff (e.g. "packages/*/dist/*"). Non-empty names the job "node-build" and emits persist_to_workspace so the image jobs can attach it; empty names it "node-test". Only applies with --language=node.`)
}

func (f *flag) Validate() error {
	if f.RepoName == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagRepoName)
	}
	if f.ForcePublic && f.ImagePrivateOnly {
		return microerror.Maskf(invalidFlagError, "--%s and --%s are mutually exclusive", flagForcePublic, flagImagePrivateOnly)
	}
	if f.SkipATS && f.ATSBranchOnly {
		return microerror.Maskf(invalidFlagError, "--%s and --%s are mutually exclusive", flagSkipATS, flagATSBranchOnly)
	}
	if f.SkipATS && f.ATSVersion != "" {
		return microerror.Maskf(invalidFlagError, "--%s and --%s are mutually exclusive", flagSkipATS, flagATSVersion)
	}

	return nil
}
