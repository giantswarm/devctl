package cancel

import "github.com/spf13/cobra"

type flag struct {
	Pipeline bool
	Stale    bool
	DryRun   bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.Pipeline, "pipeline", false, "Treat the ID as a pipeline ID; cancel all active workflows")
	cmd.Flags().BoolVar(&f.Stale, "stale", false, "Cancel queued workflows of obsolete branch pipelines across the repository")
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Report cancellation candidates without writing")
}
