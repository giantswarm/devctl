package approvealign

import (
	"github.com/spf13/cobra"
)

type flag struct {
	DryRun bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Only show what would be done without making changes")
}

func (f *flag) Validate() error {
	return nil
}
