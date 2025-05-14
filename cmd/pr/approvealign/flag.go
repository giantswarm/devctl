package approvealign

import (
	"github.com/spf13/cobra"
)

type flag struct {
	// No flags for this command
}

func (f *flag) Init(cmd *cobra.Command) {
	// No flags to initialize
}

func (f *flag) Validate() error {
	// No flags to validate
	return nil
} 