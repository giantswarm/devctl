package merge

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

const (
	flagTimeout        = "timeout"
	flagReleaseTimeout = "release-timeout"
	flagNoReleaseWait  = "no-release-wait"
	flagRebase         = "rebase"
	flagUpdateBranch   = "update-branch"
)

type flag struct {
	Timeout        time.Duration
	ReleaseTimeout time.Duration
	NoReleaseWait  bool
	Rebase         bool
	UpdateBranch   bool
	Progress       bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.Timeout, flagTimeout, prwait.DefaultTimeout, "How long to wait for the CI outcome (and a merge queue) before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().DurationVar(&f.ReleaseTimeout, flagReleaseTimeout, releasewait.DefaultTimeout, "How long to wait after the merge for its release to be pullable before exit 9; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().BoolVar(&f.NoReleaseWait, flagNoReleaseWait, false, "End at the merge instead of waiting for the release it triggers (devctl release wait --pr does that wait on its own)")
	cmd.Flags().BoolVar(&f.Rebase, flagRebase, false, "Rebase-merge instead of squash-merging (repositories whose convention is one commit per patch)")
	cmd.Flags().BoolVar(&f.UpdateBranch, flagUpdateBranch, false, "A head behind a strict base is updated from the base and the new head waited for, instead of exit 3")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
