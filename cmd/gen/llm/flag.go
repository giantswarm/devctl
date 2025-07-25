package llm

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v7/pkg/gen"
)

const (
	flagFlavour  = "flavour"
	flagLanguage = "language"
)

type flag struct {
	Flavours gen.FlavourSlice
	Language string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, gen.FlavourSlice{}), flagFlavour, "f", fmt.Sprintf(`The type of project that you want to generate rules for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", "Language of the repo, for generating additional language-specific rules.")
}

func (f *flag) Validate() error {
	// Always generate a base rule set.

	return nil
}
