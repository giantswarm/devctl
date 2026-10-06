package merge

import (
	"fmt"
	"os"
	"strconv"
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
	flagDetach         = "detach"
	flagOnDone         = "on-done"
	// flagDetachedHandle is the hidden flag --detach gives the merge's own
	// process: the handle whose directory takes its document.
	flagDetachedHandle = "detached-handle"
)

type flag struct {
	Timeout        time.Duration
	ReleaseTimeout time.Duration
	NoReleaseWait  bool
	Rebase         bool
	UpdateBranch   bool
	Dispatch       string
	Detach         bool
	OnDone         string
	DetachedHandle string
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
	cmd.Flags().BoolVar(&f.Detach, flagDetach, false, "Start the merge in a process of its own and return at once with its handle; /home/teemow/.go/bin/beekeeper gate -- devctl pr merge status <handle> reads the outcome")
	cmd.Flags().StringVar(&f.OnDone, flagOnDone, "", "With --detach: a shell command run when the detached merge ended, with DEVCTL_MERGE_HANDLE, DEVCTL_MERGE_EXIT_CODE, DEVCTL_MERGE_DOCUMENT, DEVCTL_MERGE_REPOSITORY and DEVCTL_MERGE_NUMBER set")
	cmd.Flags().StringVar(&f.DetachedHandle, flagDetachedHandle, "", "The handle of the detached merge this process runs (set by --detach)")
	_ = cmd.Flags().MarkHidden(flagDetachedHandle)
	agentcli.ProgressFlag(cmd, &f.Progress)
	f.FailedLog.Init(cmd)
}

// childArgs is the command line of the detached merge's own process: the
// same merge, blocking, with --progress for its log and the handle whose
// directory takes its document. A dispatch from the environment is
// inherited with it.
func (f *flag) childArgs(repository string, number int, handle string) []string {
	argv := []string{"pr", "merge", repository, strconv.Itoa(number),
		"--" + flagTimeout + "=" + f.Timeout.String(),
		"--" + flagReleaseTimeout + "=" + f.ReleaseTimeout.String(),
		"--progress",
		"--" + flagDetachedHandle + "=" + handle,
	}
	for _, b := range []struct {
		name string
		on   bool
	}{{flagNoReleaseWait, f.NoReleaseWait}, {flagRebase, f.Rebase}, {flagUpdateBranch, f.UpdateBranch}} {
		if b.on {
			argv = append(argv, "--"+b.name)
		}
	}
	if f.Dispatch != "" {
		argv = append(argv, "--"+flagDispatch+"="+f.Dispatch)
	}
	if f.FailedLog.Enabled {
		argv = append(argv, "--failed-log", "--failed-log-lines="+strconv.Itoa(f.FailedLog.Lines))
	}
	if f.OnDone != "" {
		argv = append(argv, "--"+flagOnDone+"="+f.OnDone)
	}
	return argv
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
