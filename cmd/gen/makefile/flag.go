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
	Flavour  string
	Language string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Flavour, flagFlavour, "f", "", fmt.Sprintf(`The type of project that you want to generate the Makefile for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().StringVarP(&f.Language, flagLanguage, "l", "", fmt.Sprintf(`The programming language of project that you want to generate the Makefile for. Possible values: <%s>`, strings.Join(gen.AllLanguages(), "|")))
}

func (f *flag) Validate() error {
	if !gen.IsValidFlavour(f.Flavour) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagFlavour, strings.Join(gen.AllFlavours(), "|"))
	}
	if !gen.IsValidLanguage(f.Language) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagLanguage, strings.Join(gen.AllLanguages(), "|"))
	}

	return nil
}
