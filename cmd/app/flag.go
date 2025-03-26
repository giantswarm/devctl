package app

import (
	"github.com/spf13/cobra"
)

type flag struct{}

func (f *flag) Init(cmd *cobra.Command) {
	// No flags for the app command itself
	// Subcommands will have their own flags
}

func (f *flag) Validate() error {
	return nil
}
