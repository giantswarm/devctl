// Package write is what the team-file writes share -- adopt, update,
// transfer, set-lifecycle: one call to a write tool of
// giantswarm-repo-manager with dryRun or mode commit, the plan printed on a
// dry run and the outcome on a commit.
package write

import (
	"context"
	"io"
	"time"

	"github.com/giantswarm/microerror"
	"github.com/spf13/cobra"

	"github.com/giantswarm/devctl/v8/cmd/repo/internal/client"
	"github.com/giantswarm/devctl/v8/pkg/reposetup/manager"
)

// CallTimeout bounds a write: the manager's writes run on GitHub within the
// call and may take up to a minute.
const CallTimeout = 2 * time.Minute

// Flags are the flags every write takes: the output, --dry-run and --reason.
type Flags struct {
	client.Flags
	DryRun bool
	Reason string
}

// Init registers the flags; what says what the dry run leaves unwritten.
func (f *Flags) Init(cmd *cobra.Command, what string) {
	f.Flags.Init(cmd)
	cmd.Flags().BoolVar(&f.DryRun, "dry-run", false, "Print the rendered change, the pull request and the ask as they would be; "+what+".")
	cmd.Flags().StringVar(&f.Reason, "reason", "", "Why, for the pull request body and the ask.")
}

// Args completes a write's arguments with the reason and dryRun or mode.
func (f *Flags) Args(args map[string]any) map[string]any {
	if f.Reason != "" {
		args["reason"] = f.Reason
	}
	return client.WriteArgs(args, f.DryRun)
}

// Run calls tool with args and prints the plan (a dry run) or the outcome
// (a commit), or the answer as it came.
func Run(ctx context.Context, session *client.Session, w io.Writer, f *Flags, tool string, args map[string]any) error {
	callCtx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	payload, err := session.Call(callCtx, tool, f.Args(args))
	if err != nil {
		return microerror.Mask(err)
	}
	if f.JSON() {
		return microerror.Mask(client.PrintJSON(w, payload))
	}
	if f.DryRun {
		plan, err := client.Decode[manager.Plan](tool, payload)
		if err != nil {
			return microerror.Mask(err)
		}
		client.PrintPlan(w, plan)
		return nil
	}
	committed, err := client.Decode[manager.Committed](tool, payload)
	if err != nil {
		return microerror.Mask(err)
	}
	client.PrintCommitted(w, committed)
	return nil
}
