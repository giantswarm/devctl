package rerun

import "github.com/spf13/cobra"

const (
	flagFromFailed = "from-failed"
	flagCancel     = "cancel"
)

type flag struct {
	FromFailed bool
	Cancel     bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.FromFailed, flagFromFailed, false, "Rerun the failed jobs and the jobs that depend on them, keeping the passed ones; off, every job runs again")
	cmd.Flags().BoolVar(&f.Cancel, flagCancel, false, "Cancel a workflow still running before the rerun, the recovery of a stuck one; off, a running workflow is refused")
}
