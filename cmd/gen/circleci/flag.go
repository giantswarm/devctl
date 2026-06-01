package circleci

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/gen"
	"github.com/giantswarm/devctl/v8/pkg/gen/input/circleci"
)

const (
	flagFlavour    = "flavour"
	flagLanguage   = "language"
	flagRepoName   = "repo-name"
	flagOrbVersion = "orb-version"
)

type flag struct {
	Flavours   gen.FlavourSlice
	Language   gen.Language
	RepoName   string
	OrbVersion string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`List of project flavours. The "app" flavour selects the chart pipeline. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().VarP(gen.NewLanguageFlagValue(&f.Language, gen.Language("")), flagLanguage, "l", fmt.Sprintf(`The programming language. "go" selects the go-build job. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
	cmd.Flags().StringVarP(&f.RepoName, flagRepoName, "r", "", "Repository name under the giantswarm organization (used for the binary, chart, and job names).")
	cmd.Flags().StringVar(&f.OrbVersion, flagOrbVersion, circleci.DefaultOrbVersion, "The giantswarm/architect orb version to pin.")
}

func (f *flag) Validate() error {
	if f.RepoName == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagRepoName)
	}

	return nil
}
