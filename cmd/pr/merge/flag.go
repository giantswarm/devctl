package merge

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/prmerge"
	"github.com/giantswarm/devctl/v8/pkg/prwait"
	"github.com/giantswarm/devctl/v8/pkg/releasewait"
)

const (
	flagTimeout        = "timeout"
	flagReleaseTimeout = "release-timeout"
	flagNoReleaseWait  = "no-release-wait"
	flagRebase         = "rebase"
	flagUpdateBranch   = "update-branch"
	flagDispatch       = "dispatch"
)

type flag struct {
	Timeout        time.Duration
	ReleaseTimeout time.Duration
	NoReleaseWait  bool
	Rebase         bool
	UpdateBranch   bool
	Dispatch       string
	Progress       bool
	FailedLog      prwait.FailedLog
	// getenv reads DEVCTL_MERGE_DISPATCH; tests replace it.
	getenv func(string) string
}

func (f *flag) Init(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&f.Timeout, flagTimeout, prwait.DefaultTimeout, "How long to wait for the CI outcome (and a merge queue) before exit 2; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().DurationVar(&f.ReleaseTimeout, flagReleaseTimeout, releasewait.DefaultTimeout, "How long to wait after the merge for its release to be pullable before exit 9; scaled by DEVCTL_TIME_SCALE")
	cmd.Flags().BoolVar(&f.NoReleaseWait, flagNoReleaseWait, false, "End at the merge instead of waiting for the release it triggers (devctl release wait --pr does that wait on its own)")
	cmd.Flags().BoolVar(&f.Rebase, flagRebase, false, "Rebase-merge instead of squash-merging (repositories whose convention is one commit per patch)")
	cmd.Flags().BoolVar(&f.UpdateBranch, flagUpdateBranch, false, "A head behind its base is updated from the base before its checks are judged, and the new head waited for; behind a strict base, instead of exit 3")
	cmd.Flags().StringVar(&f.Dispatch, flagDispatch, "", "A workflow to dispatch after the merge, <owner>/<repo>/<workflow file>[@<ref>], with the inputs repository, pull_request and release; a failed dispatch is a warning. Default: $"+prmerge.EnvDispatch)
	agentcli.ProgressFlag(cmd, &f.Progress)
	f.FailedLog.Init(cmd)
}

// dispatch is the workflow to dispatch after the merge: the flag, or the
// environment's when the flag is not given; nil when neither is set.
func (f *flag) dispatch() (*prmerge.Dispatch, error) {
	target := f.Dispatch
	if target == "" {
		getenv := f.getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		target = getenv(prmerge.EnvDispatch)
	}
	if target == "" {
		return nil, nil
	}
	d, err := prmerge.ParseDispatch(target)
	if err != nil {
		return nil, fmt.Errorf("--%s (or $%s): %w", flagDispatch, prmerge.EnvDispatch, err)
	}
	return d, nil
}
