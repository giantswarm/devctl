package update

import (
	"github.com/spf13/cobra"
)

const (
	flagNoCache = "no-cache"
)

type flag struct {
	NoCache bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.NoCache, flagNoCache, false, "Disable version cache.")
}

func (f *flag) Validate() error {
	return nil
}
