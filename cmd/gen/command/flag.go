package command

import (
	"github.com/spf13/cobra"
)

type flag struct {
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Args = validatePositionalArgs
}

func (f *flag) Validate() error {
	return nil
}

func validatePositionalArgs(cmd *cobra.Command, args []string) error {
	return nil
}
