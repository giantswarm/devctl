package makefile

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagFlavour  = "flavour"
	flagLanguage = "language"
)

type flag struct {
	Flavours gen.FlavourSlice
	Language gen.Language
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().VarP(gen.NewFlavourSliceFlagValue(&f.Flavours, []gen.Flavour{}), flagFlavour, "f", fmt.Sprintf(`List of types of project that you want to generate the Makefile for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().VarP(gen.NewLanguageFlagValue(&f.Language, gen.Language("")), flagLanguage, "l", fmt.Sprintf(`The programming language of project that you want to generate the Makefile for. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
}

func (f *flag) Validate() error {
	if len(f.Flavours) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one or more of: %s", flagFlavour, strings.Join(gen.AllFlavours(), ", ")
	}
	if len(f.Language) == 0 {
		return microerror.Maskf(invalidFlagError, "--%s must be one of: %s", flagLanguage, strings.Join(gen.AllLanguages(), ", "))
	}

	if f.Flavours.Contains(gen.FlavourCLI) && f.Language != gen.LanguageGo {
		return microerror.Maskf(
			invalidFlagError,
			"flavour %q is supported only for language %q",
			gen.FlavourCLI, gen.LanguageGo,
		)
	}

	return nil
}
