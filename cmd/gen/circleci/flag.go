package circleci

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	flagAppCatalog       = "app-catalog"
	flagAppCatalogTest   = "app-catalog-test"
	flagBranchPublish    = "branch-publish"
	flagBuildConcurrency = "build-concurrency"
	flagChartName        = "chart-name"
	flagForcePublic      = "force-public"
	flagImagePreBuildJob = "image-pre-build-job"
	flagImagePrivateOnly = "image-private-only"
	flagImageName        = "image-name"
	flagImagePlatforms   = "image-platforms"
	flagImageDockerfile  = "image-dockerfile"
	flagResourceClass    = "resource-class"
	flagSkipATS          = "skip-ats"
	flagFlavour          = "flavour"
	flagLanguage         = "language"
	flagRepoName         = "repo-name"
	flagPackageManager   = "package-manager"
	flagNodeTestTarget   = "node-test-target"
	flagNodeBuildTarget  = "node-build-target"
	flagNodeBuildOutput  = "node-build-output"
)

type flag struct {
	AppCatalog       string
	AppCatalogTest   string
	BranchPublish    bool
	BuildConcurrency string
	ChartName        string
	ForcePublic      bool
	ImagePreBuildJob string
	ImagePrivateOnly bool
	ImageName        string
	ImagePlatforms   string
	ImageDockerfile  string
	ResourceClass    string
	SkipATS          bool
	Flavours         gen.FlavourSlice
	Language         gen.Language
	RepoName         string
	PackageManager   string
	NodeTestTarget   string
	NodeBuildTarget  string
	NodeBuildOutput  string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.AppCatalog, flagAppCatalog, "", `Catalog the chart pipeline publishes to (push-to-app-catalog app_catalog). Empty defaults to "giantswarm-catalog"; set it for repos that ship to a different catalog (e.g. the internal "giantswarm-operations-platform") so generation does not migrate the chart to the public catalog.`)
	cmd.Flags().StringVar(&f.AppCatalogTest, flagAppCatalogTest, "", `Test catalog the chart pipeline publishes to (push-to-app-catalog app_catalog_test). Empty defaults to "giantswarm-test-catalog". Kept paired with --app-catalog.`)
	cmd.Flags().BoolVar(&f.BranchPublish, flagBranchPublish, false, "Publish a dev image and chart on branch builds. By default branches build + test only (no push); when set, the branch path additionally pushes an amd64 dev image and the dev chart (coupled).")
	cmd.Flags().StringVar(&f.BuildConcurrency, flagBuildConcurrency, "", `Override how many architectures the cli-flavour go-build job compiles concurrently (architect go-build "build_concurrency" param). Empty defaults to "auto" (nproc). Lower it (e.g. "2") for repos whose binary is large enough that a cold full-matrix cross-compile OOMs the runner at "auto" -- memory, not CPU, is the binding constraint, and a killed build never stores the build cache. Only applies to the cli flavour.`)
	cmd.Flags().StringVar(&f.ChartName, flagChartName, "", "Override the chart name (the push-to-app-catalog `chart` param and the helm/<chart> directory). Empty defaults to the repo name. Set it for repos whose chart directory does not match the repo name (e.g. docs-proxy -> docs-proxy-app). The append-only custom.yml merge cannot rename a generated job's chart.")
	cmd.Flags().BoolVar(&f.ForcePublic, flagForcePublic, false, "Push the image and chart as public artifacts even though the repo is private (architect `force-public: true`). Set it for private repos that publish public artifacts (e.g. web-assets). Mutually exclusive with --image-private-only. The append-only custom.yml merge cannot add this to a generated job.")
	cmd.Flags().StringVar(&f.ImagePreBuildJob, flagImagePreBuildJob, "", "Name of a repo-owned job (defined in .circleci/custom.yml) the release image build must wait on. Adds a `requires` entry to push-to-registries-release, which the append-only custom.yml merge cannot inject into a generated job. Used for workspace-handoff pre-steps. Empty for the common case.")
	cmd.Flags().BoolVar(&f.ImagePrivateOnly, flagImagePrivateOnly, false, "Ship the image to the private registry only (gsociprivate), replacing split-china-push and omitting the sync-china-registry job. Set it for private repos whose image must not land in the public catalog.")
	cmd.Flags().StringVar(&f.ImageName, flagImageName, "", "Override the `giantswarm/<repo>` default image name on the image jobs (push-to-registries / sync-china-registry `image` param). Set it for repos whose published image differs from the repo name (e.g. kserve -> giantswarm/kserve-controller). The append-only custom.yml merge cannot rename a generated job's image. Empty keeps the orb default.")
	cmd.Flags().StringVar(&f.ImagePlatforms, flagImagePlatforms, "", "Override the buildx platform list on the image jobs (push-to-registries `platforms` param). Empty lets the orb default apply (linux/amd64,linux/arm64 when no go-build .platforms file). Set it for single-architecture images (e.g. vllm -> linux/arm64, whose amd64 build has no prebuilt wheels).")
	cmd.Flags().StringVar(&f.ImageDockerfile, flagImageDockerfile, "", "Override the Dockerfile path on the image jobs (push-to-registries `dockerfile` param). Set it for repos whose Dockerfile is not at the repo root (e.g. backstage -> packages/backend/Dockerfile); a non-empty value also turns the image pipeline on, since the root-Dockerfile derivation misses a nested Dockerfile. The append-only custom.yml merge cannot set this on a generated job. Empty keeps the orb default.")
	cmd.Flags().StringVar(&f.ResourceClass, flagResourceClass, "", `Override the CircleCI resource_class on the cli-flavour go-build job. Empty defaults to "large". Raise it (e.g. "xlarge") for repos that need more RAM/CPU headroom for the cold cross-compile. Only applies to the cli flavour.`)
	cmd.Flags().BoolVar(&f.SkipATS, flagSkipATS, false, `Opt the chart pipeline out of app-test-suite (ATS) chart tests. By default an "app" flavour repo runs architect/run-tests-with-ats between build-chart and the chart push, and generation emits the canonical tests/ats/Pipfile. When set, those test jobs and the Pipfile are not generated and the chart push gates directly on build-chart. Only applies to the app flavour.`)
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`List of project flavours. The "app" flavour selects the chart pipeline. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().VarP(gen.NewLanguageFlagValue(&f.Language, gen.Language("")), flagLanguage, "l", fmt.Sprintf(`The programming language. "go" selects the go-build job. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
	cmd.Flags().StringVarP(&f.RepoName, flagRepoName, "r", "", "Repository name under the giantswarm organization (used for the binary, chart, and job names).")
	cmd.Flags().StringVar(&f.PackageManager, flagPackageManager, "", `Node package manager for the build/test job (one of "npm", "yarn", "yarn-classic", "pnpm"). Empty detects it from the lockfile (package-lock.json -> npm, pnpm-lock.yaml -> pnpm, yarn.lock -> yarn Berry or yarn-classic by its header). Only applies with --language=node.`)
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

	return nil
}
