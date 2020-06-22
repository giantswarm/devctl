package makefile

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flavour = "flavour"

	flavourApp      = "app"
	flavourCLI      = "cli"
	flavourOperator = "operator"
)

type flag struct {
	Flavour string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.Flavour, flavour, "f", flavourOperator, `The type of project that you want to generate the Makefile for. Possible values: <app|cli|operator>`)
}

func (f *flag) Validate() error {
	if f.Flavour != flavourApp && f.Flavour != flavourCLI && f.Flavour != flavourOperator {
		return microerror.Maskf(invalidFlagError, "--%s must be one of <app|cli|operator>", flavour)
	}

	return nil
}
