package workflows

import (
	"fmt"
	"strings"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/pkg/gen"
)

const (
	flagCheckSecrets = "check-secrets"
	flagFlavour      = "flavour"
)

type flag struct {
	CheckSecrets bool
	Flavour      string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Flavour, flagFlavour, "f", "", fmt.Sprintf(`The type of project that you want to generate the workflows for. Possible values: <%s>`, strings.Join(gen.AllFlavours(), "|")))
	cmd.Flags().BoolVar(&f.CheckSecrets, flagCheckSecrets, true, "If true, also generate a secret-scanning workflow. Possible values: true (default), false.")
}

func (f *flag) Validate() error {
	if !gen.IsValidFlavour(f.Flavour) {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <%s>", flagFlavour, strings.Join(gen.AllFlavours(), "|"))
	}

	return nil
}
