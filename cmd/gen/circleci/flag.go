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
	flagImagePreBuildJob = "image-pre-build-job"
	flagImagePrivateOnly = "image-private-only"
	flagImageName        = "image-name"
	flagImagePlatforms   = "image-platforms"
	flagFlavour          = "flavour"
	flagLanguage         = "language"
	flagRepoName         = "repo-name"
)

type flag struct {
	AppCatalog       string
	AppCatalogTest   string
	BranchPublish    bool
	ImagePreBuildJob string
	ImagePrivateOnly bool
	ImageName        string
	ImagePlatforms   string
	Flavours         gen.FlavourSlice
	Language         gen.Language
	RepoName         string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.AppCatalog, flagAppCatalog, "", `Catalog the chart pipeline publishes to (push-to-app-catalog app_catalog). Empty defaults to "giantswarm-catalog"; set it for repos that ship to a different catalog (e.g. the internal "giantswarm-operations-platform") so generation does not migrate the chart to the public catalog.`)
	cmd.Flags().StringVar(&f.AppCatalogTest, flagAppCatalogTest, "", `Test catalog the chart pipeline publishes to (push-to-app-catalog app_catalog_test). Empty defaults to "giantswarm-test-catalog". Kept paired with --app-catalog.`)
	cmd.Flags().BoolVar(&f.BranchPublish, flagBranchPublish, false, "Publish a dev image and chart on branch builds. By default branches build + test only (no push); when set, the branch path additionally pushes an amd64 dev image and the dev chart (coupled).")
	cmd.Flags().StringVar(&f.ImagePreBuildJob, flagImagePreBuildJob, "", "Name of a repo-owned job (defined in .circleci/custom.yml) the release image build must wait on. Adds a `requires` entry to push-to-registries-release, which the append-only custom.yml merge cannot inject into a generated job. Used for workspace-handoff pre-steps. Empty for the common case.")
	cmd.Flags().BoolVar(&f.ImagePrivateOnly, flagImagePrivateOnly, false, "Ship the image to the private registry only (gsociprivate), replacing split-china-push and omitting the sync-china-registry job. Set it for private repos whose image must not land in the public catalog.")
	cmd.Flags().StringVar(&f.ImageName, flagImageName, "", "Override the `giantswarm/<repo>` default image name on the image jobs (push-to-registries / sync-china-registry `image` param). Set it for repos whose published image differs from the repo name (e.g. kserve -> giantswarm/kserve-controller). The append-only custom.yml merge cannot rename a generated job's image. Empty keeps the orb default.")
	cmd.Flags().StringVar(&f.ImagePlatforms, flagImagePlatforms, "", "Override the buildx platform list on the image jobs (push-to-registries `platforms` param). Empty lets the orb default apply (linux/amd64,linux/arm64 when no go-build .platforms file). Set it for single-architecture images (e.g. vllm -> linux/arm64, whose amd64 build has no prebuilt wheels).")
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`List of project flavours. The "app" flavour selects the chart pipeline. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().VarP(gen.NewLanguageFlagValue(&f.Language, gen.Language("")), flagLanguage, "l", fmt.Sprintf(`The programming language. "go" selects the go-build job. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
	cmd.Flags().StringVarP(&f.RepoName, flagRepoName, "r", "", "Repository name under the giantswarm organization (used for the binary, chart, and job names).")
}

func (f *flag) Validate() error {
	if f.RepoName == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagRepoName)
	}

	return nil
}
