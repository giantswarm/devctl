package makefile

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	app = "app"
)

type flag struct {
	App    string
	Output string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.App, app, "", `Name of the application, this will be the name of the go binary.`)
	cmd.Flags().StringVar(&f.Output, "output", "", `Where you want the generated Makefile to be saved`)
}

func (f *flag) Validate() error {
	if f.App == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", app)
	}

	return nil
}
