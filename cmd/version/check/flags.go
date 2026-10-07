package check

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
	cmd.Flags().BoolVar(&f.NoCache, flagNoCache, false, "Do not refresh the version cache with the answer; the latest version is asked for either way")
}

func (f *flag) Validate() error {
	return nil
}
