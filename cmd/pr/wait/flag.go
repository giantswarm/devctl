package wait

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

const flagTimeout = "timeout"

type flag struct {
	Timeout  time.Duration
	Progress bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.Timeout, flagTimeout, prwait.DefaultTimeout, "How long to wait for an outcome before exit 2; scaled by DEVCTL_TIME_SCALE")
	agentcli.ProgressFlag(cmd, &f.Progress)
}
