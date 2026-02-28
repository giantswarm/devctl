package approvemergerenovate

import (
	"github.com/spf13/cobra"
)

type flag struct {
	DryRun bool
	Watch  bool
	ByRepo bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Only show what would be done without making changes")
	cmd.Flags().BoolVarP(&f.Watch, "watch", "w", false, "Keep running and watch for new PRs (poll every minute, exit with Ctrl+C)")
	cmd.Flags().BoolVar(&f.ByRepo, "by-repo", false, "In interactive mode, group PRs by repository instead of dependency name")
}

func (f *flag) Validate() error {
	return nil
}
