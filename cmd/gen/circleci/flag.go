package circleci

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
)

const (
	flagBranchPublish   = "branch-publish"
	flagFlavour         = "flavour"
	flagLanguage        = "language"
	flagRepoName        = "repo-name"
	flagReleaseBinaries = "release-binaries"
)

type flag struct {
	BranchPublish   bool
	Flavours        gen.FlavourSlice
	Language        gen.Language
	RepoName        string
	ReleaseBinaries bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.BranchPublish, flagBranchPublish, false, "Publish a dev image and chart on branch builds. By default branches build + test only (no push); when set, the branch path additionally pushes an amd64 dev image and the dev chart (coupled).")
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`List of project flavours. The "app" flavour selects the chart pipeline. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().VarP(gen.NewLanguageFlagValue(&f.Language, gen.Language("")), flagLanguage, "l", fmt.Sprintf(`The programming language. "go" selects the go-build job. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
	cmd.Flags().StringVarP(&f.RepoName, flagRepoName, "r", "", "Repository name under the giantswarm organization (used for the binary, chart, and job names).")
	cmd.Flags().BoolVar(&f.ReleaseBinaries, flagReleaseBinaries, false, "Build cross-platform Go binaries and attach them to the GitHub Release on tag builds. Adds the architectures matrix to go-build and an upload-release-assets job, and caps the image push to linux/amd64,linux/arm64. Requires --language=go. Possible values: false (default), true.")
}

func (f *flag) Validate() error {
	if f.RepoName == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagRepoName)
	}

	if f.ReleaseBinaries && f.Language != gen.LanguageGo {
		return microerror.Maskf(invalidFlagError, "--%s requires --%s=go (it attaches the go-build binary to the GitHub Release)", flagReleaseBinaries, flagLanguage)
	}

	return nil
}
