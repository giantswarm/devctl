package wait

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
)

const flagTimeout = "timeout"

type flag struct {
	Timeout              time.Duration
	Progress             bool
	FailedLog            prwait.FailedLog
	CircleCIFromStatuses bool
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.Timeout, flagTimeout, prwait.DefaultTimeout, "How long to wait for an outcome before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().BoolVar(&f.CircleCIFromStatuses, "circleci-from-statuses", false, "Read CircleCI jobs from the head's configuration and GitHub commit statuses without a CircleCI token; --failed-log uses the CircleCI API instead")
	agentcli.ProgressFlag(cmd, &f.Progress)
	f.FailedLog.Init(cmd)
}
