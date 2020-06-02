package makefile

import (
	"github.com/spf13/cobra"

	"github.com/giantswarm/microerror"
)

const (
	app = "app"
)

type flag struct {
	Dir         string
	Application string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Application, app, "", `Name of the application, this will be the name of the go binary.`)
}

func (f *flag) Validate() error {
	if f.Application == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", app)
	}

	return nil
}
