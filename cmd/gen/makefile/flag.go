package makefile

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagFlavour = "flavour"
)

type flag struct {
	Flavour string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Flavour, flagFlavour, "f", "", fmt.Sprintf(`The type of project that you want to generate the Makefile for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
}

func (f *flag) Validate() error {
	if !gen.IsValidFlavour(f.Flavour) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagFlavour, strings.Join(gen.AllFlavours(), "|"))
	}

	return nil
}
