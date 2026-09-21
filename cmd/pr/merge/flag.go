package merge

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

const (
	flagTimeout      = "timeout"
	flagRebase       = "rebase"
	flagUpdateBranch = "update-branch"
)

type flag struct {
	Timeout      time.Duration
	Rebase       bool
	UpdateBranch bool
	Progress     bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.Timeout, flagTimeout, prwait.DefaultTimeout, "How long to wait for an outcome before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().BoolVar(&f.Rebase, flagRebase, false, "Rebase-merge instead of squash-merging (repositories whose convention is one commit per patch)")
	cmd.Flags().BoolVar(&f.UpdateBranch, flagUpdateBranch, false, "A head behind a strict base is updated from the base and the new head waited for, instead of exit 3")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
