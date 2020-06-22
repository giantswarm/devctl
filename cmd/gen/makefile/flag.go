package makefile

import (
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
	return nil
}
