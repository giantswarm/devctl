package renovate

import "github.com/spf13/cobra"

type flag struct {
	Remove bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.Remove, "remove", false, "Set this flag to disable Renovate access for the repo. Otherwise it will be enabled.")
}

func (f *flag) Validate() error {
	return nil
}
