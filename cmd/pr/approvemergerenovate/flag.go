package approvemergerenovate

import (
	"fmt"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"
)

const (
	GroupingDependency = "dependency"
	GroupingRepo       = "repo"
)

type flag struct {
	DryRun   bool
	Watch    bool
	Grouping string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Only show what would be done without making changes")
	cmd.Flags().BoolVarP(&f.Watch, "watch", "w", false, "Keep running and watch for new PRs (poll every minute, exit with Ctrl+C)")
	cmd.Flags().StringVar(&f.Grouping, "grouping", GroupingDependency, fmt.Sprintf("In interactive mode, group PRs by %q or %q", GroupingDependency, GroupingRepo))
}

func (f *flag) Validate() error {
	switch f.Grouping {
	case GroupingDependency, GroupingRepo:
	default:
		return microerror.Maskf(invalidFlagsError, "--grouping must be %q or %q, got %q", GroupingDependency, GroupingRepo, f.Grouping)
	}
	return nil
}
