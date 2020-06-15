package workflows

import (
	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	flagDir = "dir"
)

type flag struct {
	Dir string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.Dir, flagDir, "./.github/workflows", `Directory where the generated code should be located.`)
}

func (f *flag) Validate() error {
	if f.Dir == "" {
		return microerror.Maskf(invalidFlagError, "--%s must not be empty", flagDir)
	}

	return nil
}
